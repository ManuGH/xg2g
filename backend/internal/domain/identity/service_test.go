package identity_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/identity"
	"github.com/ManuGH/xg2g/internal/domain/identity/store"
	"github.com/ManuGH/xg2g/internal/domain/identity/webauthn"
	"github.com/ManuGH/xg2g/internal/persistence/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIdentityService_FullIntegration(t *testing.T) {
	ctx := context.Background()
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "identity.sqlite")

	s, err := store.OpenSQLite(dbPath, sqlite.DefaultConfig())
	require.NoError(t, err)
	defer s.Close()

	now := time.Date(2026, 8, 12, 22, 0, 0, 0, time.UTC)
	cfg := identity.Config{
		RPID:                "xg2g.home.matrixcentral.de",
		RPName:              "xg2g TV",
		ExpectedOrigin:      "https://xg2g.home.matrixcentral.de",
		SessionTTL:          24 * time.Hour,
		PasskeyChallengeTTL: 5 * time.Minute,
	}

	svc := identity.NewService(cfg, s)
	svc.SetNowFunc(func() time.Time { return now })

	// 1. Generate & Consume Bootstrap Token
	setupToken, err := svc.GenerateBootstrapToken(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, setupToken)

	tokenValid, err := svc.ValidateAndConsumeBootstrapToken(ctx, setupToken)
	require.NoError(t, err)
	assert.True(t, tokenValid)

	// Re-using the token fails
	tokenValid2, err := svc.ValidateAndConsumeBootstrapToken(ctx, setupToken)
	require.NoError(t, err)
	assert.False(t, tokenValid2)

	// 2. Begin Bootstrap Registration (Transient, DB must remain empty!)
	regOpts, err := svc.BeginBootstrapRegistration(ctx, "manuel", "Manuel")
	require.NoError(t, err)
	assert.NotEmpty(t, regOpts.Challenge)

	hasUsersBeforeFinish, err := svc.HasUsers(ctx)
	require.NoError(t, err)
	assert.False(t, hasUsersBeforeFinish, "DB must remain empty until Passkey is successfully finished")

	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	credID := []byte("cred_integration_test")
	aaguid, _ := hex.DecodeString("0102030405060708090a0b0c0d0e0f10")

	coseMap := map[any]any{
		int64(1):  int64(2),  // kty: EC2
		int64(3):  int64(-7), // alg: ES256
		int64(-1): int64(1),  // crv: P-256
		int64(-2): privKey.X.Bytes(),
		int64(-3): privKey.Y.Bytes(),
	}
	coseBytes := encodeTestCBORMap(coseMap)

	rpIDHash := sha256.Sum256([]byte(cfg.RPID))
	authDataReg := make([]byte, 37)
	copy(authDataReg[:32], rpIDHash[:])
	authDataReg[32] = 0x01 | 0x04 | 0x40 | 0x08 | 0x10 // UP | UV | AT | BE | BS
	binary.BigEndian.PutUint32(authDataReg[33:37], 0)

	authDataReg = append(authDataReg, aaguid...)
	credIDLenBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(credIDLenBytes, uint16(len(credID)))
	authDataReg = append(authDataReg, credIDLenBytes...)
	authDataReg = append(authDataReg, credID...)
	authDataReg = append(authDataReg, coseBytes...)

	attObjMap := map[any]any{
		"fmt":      "none",
		"authData": authDataReg,
		"attStmt":  map[any]any{},
	}
	attObjBytes := encodeTestCBORMap(attObjMap)

	clientDataReg := webauthn.ClientDataJSON{
		Type:      "webauthn.create",
		Challenge: regOpts.Challenge,
		Origin:    cfg.ExpectedOrigin,
	}
	clientDataRegJSON, _ := json.Marshal(clientDataReg)

	regResp := webauthn.AttestationResponse{
		ClientDataJSON:    base64.RawURLEncoding.EncodeToString(clientDataRegJSON),
		AttestationObject: base64.RawURLEncoding.EncodeToString(attObjBytes),
		Transports:        []string{"internal"},
	}

	// 3. Finish Passkey Registration -> Atomically creates Admin, Passkey, Recovery Codes, Session
	passkey, bootstrapRes, err := svc.FinishPasskeyRegistration(ctx, regResp, "Safari TouchID", "Safari macOS", "192.168.1.50")
	require.NoError(t, err)
	require.NotNil(t, bootstrapRes)
	assert.Equal(t, "Safari TouchID", passkey.Nickname)
	admin := bootstrapRes.User
	assert.Equal(t, "manuel", admin.Username)
	assert.Equal(t, identity.RoleAdmin, admin.Role)
	recCodes := bootstrapRes.RecoveryCodes
	assert.Len(t, recCodes, 10)
	assert.NotEmpty(t, bootstrapRes.SessionID)

	// Invariant Check: IdentityReady and PublicReady MUST BE FALSE before recovery codes acknowledged
	idReadyBeforeAck, err := svc.IsIdentityReady(ctx)
	require.NoError(t, err)
	assert.False(t, idReadyBeforeAck, "IdentityReady must be false before recovery codes acknowledgment")

	pubReadyBeforeAck, err := svc.IsPublicReady(ctx)
	require.NoError(t, err)
	assert.False(t, pubReadyBeforeAck, "PublicReady must be false before recovery codes acknowledgment")

	// Acknowledge Recovery Codes
	err = svc.AcknowledgeRecoveryCodes(ctx, admin.ID)
	require.NoError(t, err)

	// Invariant Check: IdentityReady and PublicReady MUST NOW BE TRUE for valid HTTPS config
	idReadyAfterAck, err := svc.IsIdentityReady(ctx)
	require.NoError(t, err)
	assert.True(t, idReadyAfterAck, "IdentityReady must be true after recovery codes acknowledgment")

	pubReadyAfterAck, err := svc.IsPublicReady(ctx)
	require.NoError(t, err)
	assert.True(t, pubReadyAfterAck, "PublicReady must be true when identity is ready and public HTTPS is configured")

	// Invariant Check: Localhost or Non-HTTPS Config makes IdentityReady TRUE, but PublicReady FALSE
	localSvc := identity.NewService(identity.Config{
		RPID:           "localhost",
		ExpectedOrigin: "http://localhost:8088",
	}, s)
	localSvc.SetNowFunc(func() time.Time { return now })

	localIdReady, err := localSvc.IsIdentityReady(ctx)
	require.NoError(t, err)
	assert.True(t, localIdReady, "IdentityReady is true because admin, passkey, and acknowledged recovery codes exist")

	localPubReady, err := localSvc.IsPublicReady(ctx)
	require.NoError(t, err)
	assert.False(t, localPubReady, "PublicReady MUST be false when RPID is localhost or ExpectedOrigin is http")

	// 4. Login with Passkey
	loginOpts, err := svc.BeginPasskeyLogin(ctx, "manuel")
	require.NoError(t, err)
	assert.NotEmpty(t, loginOpts.Challenge)

	clientDataLogin := webauthn.ClientDataJSON{
		Type:      "webauthn.get",
		Challenge: loginOpts.Challenge,
		Origin:    cfg.ExpectedOrigin,
	}
	clientDataLoginJSON, _ := json.Marshal(clientDataLogin)
	clientDataHash := sha256.Sum256(clientDataLoginJSON)

	authDataLogin := make([]byte, 37)
	copy(authDataLogin[:32], rpIDHash[:])
	authDataLogin[32] = 0x01 | 0x04 | 0x08 | 0x10 // UP | UV | BE | BS
	binary.BigEndian.PutUint32(authDataLogin[33:37], 1)

	signedMessage := append(authDataLogin, clientDataHash[:]...)
	digest := sha256.Sum256(signedMessage)
	r, sBig, _ := ecdsa.Sign(rand.Reader, privKey, digest[:])
	sigDER, _ := asn1.Marshal(struct {
		R, S *big.Int
	}{r, sBig})

	loginResp := webauthn.AssertionResponse{
		CredentialID:      passkey.ID,
		ClientDataJSON:    base64.RawURLEncoding.EncodeToString(clientDataLoginJSON),
		AuthenticatorData: base64.RawURLEncoding.EncodeToString(authDataLogin),
		Signature:         base64.RawURLEncoding.EncodeToString(sigDER),
	}

	webSess, loggedUser, err := svc.FinishPasskeyLogin(ctx, loginResp, "Safari macOS", "192.168.1.50")
	require.NoError(t, err)
	assert.Equal(t, admin.ID, loggedUser.ID)
	assert.NotEmpty(t, webSess.SessionID)
	assert.True(t, webSess.IsActive(now))

	// 4. Validate Session (including network roaming: e.g. IP changes to hotel IP)
	now = now.Add(2 * time.Hour)
	svc.SetNowFunc(func() time.Time { return now })

	valSess, valUser, err := svc.ValidateWebSession(ctx, webSess.SessionID, "Safari macOS", "198.51.100.22")
	require.NoError(t, err)
	assert.Equal(t, admin.ID, valUser.ID)
	assert.True(t, valSess.IsActive(now))

	// 5. Login with Recovery Code
	recSess, recUser, err := svc.LoginWithRecoveryCode(ctx, "manuel", recCodes[0], "Safari iOS", "198.51.100.33")
	require.NoError(t, err)
	assert.Equal(t, admin.ID, recUser.ID)
	assert.True(t, recSess.IsActive(now))

	// Recovery code second use fails
	_, _, err = svc.LoginWithRecoveryCode(ctx, "manuel", recCodes[0], "Safari iOS", "198.51.100.33")
	require.ErrorIs(t, err, identity.ErrRecoveryCodeNotFound)

	// 6. Revoke other sessions
	err = svc.RevokeOtherWebSessions(ctx, admin.ID, webSess.SessionID)
	require.NoError(t, err)

	// recSess should now be revoked
	_, _, err = svc.ValidateWebSession(ctx, recSess.SessionID, "Safari iOS", "198.51.100.33")
	require.ErrorIs(t, err, identity.ErrSessionRevoked)

	// webSess should still be valid
	_, _, err = svc.ValidateWebSession(ctx, webSess.SessionID, "Safari macOS", "198.51.100.22")
	require.NoError(t, err)
}

func encodeTestCBORMap(m map[any]any) []byte {
	var buf []byte
	buf = append(buf, 0xa0|byte(len(m)))
	for k, v := range m {
		buf = append(buf, encodeTestCBORItem(k)...)
		buf = append(buf, encodeTestCBORItem(v)...)
	}
	return buf
}

func encodeTestCBORItem(item any) []byte {
	switch v := item.(type) {
	case int64:
		if v >= 0 {
			if v < 24 {
				return []byte{byte(v)}
			}
			return []byte{24, byte(v)}
		}
		n := -1 - v
		if n < 24 {
			return []byte{0x20 | byte(n)}
		}
		return []byte{0x38, byte(n)}
	case string:
		var b []byte
		b = append(b, 0x60|byte(len(v)))
		b = append(b, []byte(v)...)
		return b
	case []byte:
		var b []byte
		if len(v) < 24 {
			b = append(b, 0x40|byte(len(v)))
		} else {
			b = append(b, 0x58, byte(len(v)))
		}
		b = append(b, v...)
		return b
	case map[any]any:
		return encodeTestCBORMap(v)
	default:
		return nil
	}
}
