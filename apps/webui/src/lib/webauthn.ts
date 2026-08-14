// Copyright (c) 2025-2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

export function bufferToBase64URL(buffer: ArrayBuffer): string {
  if (!buffer) return '';
  const bytes = new Uint8Array(buffer);
  let binary = '';
  for (let i = 0; i < bytes.byteLength; i++) {
    const val = bytes[i];
    if (val !== undefined) {
      binary += String.fromCharCode(val & 0xff);
    }
  }
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

export function base64URLToBuffer(base64url: string): ArrayBuffer {
  if (!base64url) return new ArrayBuffer(0);
  try {
    let base64 = base64url.replace(/-/g, '+').replace(/_/g, '/');
    while (base64.length % 4 !== 0) {
      base64 += '=';
    }
    const rawData = atob(base64);
    const buffer = new Uint8Array(rawData.length);
    for (let i = 0; i < rawData.length; i++) {
      buffer[i] = rawData.charCodeAt(i);
    }
    return buffer.buffer;
  } catch {
    return new TextEncoder().encode(base64url).buffer;
  }
}

export interface PasskeyRegisterOptions {
  publicKey: {
    rp: { name: string; id?: string };
    user: { id: string; name: string; displayName: string };
    challenge: string;
    pubKeyCredParams: Array<{ type: string; alg: number }>;
    timeout?: number;
    authenticatorSelection?: {
      authenticatorAttachment?: AuthenticatorAttachment;
      residentKey?: ResidentKeyRequirement;
      userVerification?: UserVerificationRequirement;
    };
    attestation?: AttestationConveyancePreference;
    excludeCredentials?: Array<{ id: string; type: string }>;
  };
}

export interface PasskeyLoginOptions {
  publicKey: {
    challenge: string;
    timeout?: number;
    rpId?: string;
    userVerification?: UserVerificationRequirement;
    allowCredentials?: Array<{ id: string; type: string }>;
  };
}

export async function createPasskeyCredential(options: PasskeyRegisterOptions): Promise<any> {
  if (!navigator.credentials || !navigator.credentials.create) {
    throw new Error('Passkeys match zero available authenticators in this browser environment.');
  }

  const publicKeyOptions: PublicKeyCredentialCreationOptions = {
    ...options.publicKey,
    challenge: base64URLToBuffer(options.publicKey.challenge),
    user: {
      ...options.publicKey.user,
      id: base64URLToBuffer(options.publicKey.user.id),
    },
    pubKeyCredParams: options.publicKey.pubKeyCredParams.map((p) => ({
      type: 'public-key' as PublicKeyCredentialType,
      alg: p.alg,
    })),
    excludeCredentials: options.publicKey.excludeCredentials?.map((cred) => ({
      ...cred,
      id: base64URLToBuffer(cred.id),
      type: 'public-key' as PublicKeyCredentialType,
    })),
  };

  const credential = (await navigator.credentials.create({
    publicKey: publicKeyOptions,
  })) as PublicKeyCredential | null;

  if (!credential) {
    throw new Error('Passkey creation returned null.');
  }

  const response = credential.response as AuthenticatorAttestationResponse;

  return {
    id: credential.id,
    rawId: bufferToBase64URL(credential.rawId),
    type: credential.type,
    response: {
      clientDataJSON: bufferToBase64URL(response.clientDataJSON),
      attestationObject: bufferToBase64URL(response.attestationObject),
      transports: response.getTransports ? response.getTransports() : [],
    },
    clientExtensionResults: credential.getClientExtensionResults(),
  };
}

export async function getPasskeyAssertion(options: PasskeyLoginOptions, conditional = false): Promise<any> {
  if (!navigator.credentials || !navigator.credentials.get) {
    throw new Error('Passkeys are not supported in this browser environment.');
  }

  const publicKeyOptions: PublicKeyCredentialRequestOptions = {
    ...options.publicKey,
    challenge: base64URLToBuffer(options.publicKey.challenge),
    allowCredentials: options.publicKey.allowCredentials?.map((cred) => ({
      ...cred,
      id: base64URLToBuffer(cred.id),
      type: 'public-key' as PublicKeyCredentialType,
    })),
  };

  const getOptions: CredentialRequestOptions = {
    publicKey: publicKeyOptions,
  };

  if (conditional) {
    (getOptions as any).mediation = 'conditional';
  }

  const credential = (await navigator.credentials.get(getOptions)) as PublicKeyCredential | null;

  if (!credential) {
    throw new Error('Passkey login returned null.');
  }

  const response = credential.response as AuthenticatorAssertionResponse;

  return {
    id: credential.id,
    rawId: bufferToBase64URL(credential.rawId),
    type: credential.type,
    response: {
      id: credential.id,
      clientDataJSON: bufferToBase64URL(response.clientDataJSON),
      authenticatorData: bufferToBase64URL(response.authenticatorData),
      signature: bufferToBase64URL(response.signature),
      userHandle: response.userHandle ? bufferToBase64URL(response.userHandle) : undefined,
    },
    clientExtensionResults: credential.getClientExtensionResults(),
  };
}
