import { useTranslation } from 'react-i18next';
import { useAppContext } from '../../context/AppContext';
import { useSystemEntitlements } from '../../hooks/useServerQueries';
import { Button, Card, CardBody, StatusChip } from '../../components/ui';
import styles from './UnlockStatus.module.css';

export function UnlockStatus() {
  const { t } = useTranslation();
  const { auth } = useAppContext();
  const { data, error, isPending } = useSystemEntitlements(Boolean(auth.isAuthenticated));

  if (isPending && !data) {
    return (
      <div className={styles.page}>
        <h1>{t('unlock.pageTitle', { defaultValue: 'Unlock Status' })}</h1>
        <div className={styles.loading}>{t('common.loading', { defaultValue: 'Loading...' })}</div>
      </div>
    );
  }

  if (error && !data) {
    const message = error instanceof Error ? error.message : 'Unable to load unlock status.';
    return (
      <div className={styles.page}>
        <h1>{t('unlock.pageTitle', { defaultValue: 'Unlock Status' })}</h1>
        <div className={styles.error}>{message}</div>
      </div>
    );
  }

  if (!data) {
    return null;
  }

  const requiredScopes = data.requiredScopes ?? [];
  const grantedScopes = data.grantedScopes ?? [];
  const missingScopes = data.missingScopes ?? [];
  const grants = data.grants ?? [];
  const unlocked = data.unlocked ?? false;
  const productName = data.productName?.trim() || 'xg2g Unlock';
  const purchaseUrl = data.purchaseUrl?.trim();

  return (
    <div className={styles.page}>
      <div className={styles.header}>
        <div>
          <p className={styles.kicker}>{t('unlock.eyebrow', { defaultValue: 'Unlock Required' })}</p>
          <h1>{t('unlock.pageTitle', { defaultValue: 'Unlock Status' })}</h1>
          <p className={styles.subtitle}>
            {t('unlock.pageSubtitle', {
              defaultValue: 'This page shows which commercial scopes are still missing before the playback surface opens.',
            })}
          </p>
        </div>
        <StatusChip
          state={unlocked ? 'success' : 'warning'}
          label={unlocked
            ? t('unlock.active', { defaultValue: `${productName} active` })
            : t('unlock.pending', { defaultValue: `${productName} still locked` })}
        />
      </div>

      <div className={styles.grid}>
        <Card className={styles.card}>
          <CardBody className={styles.cardBody}>
            <p className={styles.cardEyebrow}>{t('unlock.requiredEyebrow', { defaultValue: 'Required Scopes' })}</p>
            <h2 className={styles.cardTitle}>{productName}</h2>
            <ul className={styles.scopeList}>
              {requiredScopes.length > 0 ? requiredScopes.map((scope) => (
                <li key={scope} className={styles.scopeItem}>{scope}</li>
              )) : (
                <li className={styles.scopeItem}>{t('unlock.noneRequired', { defaultValue: 'No unlock scopes configured.' })}</li>
              )}
            </ul>
          </CardBody>
        </Card>

        <Card className={styles.card}>
          <CardBody className={styles.cardBody}>
            <p className={styles.cardEyebrow}>{t('unlock.grantedEyebrow', { defaultValue: 'Granted Now' })}</p>
            <h2 className={styles.cardTitle}>{t('unlock.grantedTitle', { defaultValue: 'Active Access' })}</h2>
            <ul className={styles.scopeList}>
              {grantedScopes.length > 0 ? grantedScopes.map((scope) => (
                <li key={scope} className={styles.scopeItem}>{scope}</li>
              )) : (
                <li className={styles.scopeItem}>{t('unlock.noneGranted', { defaultValue: 'No active unlock scopes yet.' })}</li>
              )}
            </ul>
          </CardBody>
        </Card>

        <Card className={styles.card}>
          <CardBody className={styles.cardBody}>
            <p className={styles.cardEyebrow}>{t('unlock.missingEyebrow', { defaultValue: 'Still Missing' })}</p>
            <h2 className={styles.cardTitle}>{t('unlock.missingTitle', { defaultValue: 'What Blocks Playback' })}</h2>
            <ul className={styles.scopeList}>
              {missingScopes.length > 0 ? missingScopes.map((scope) => (
                <li key={scope} className={styles.scopeItem}>{scope}</li>
              )) : (
                <li className={styles.scopeItem}>{t('unlock.noneMissing', { defaultValue: 'Nothing missing. Playback can open.' })}</li>
              )}
            </ul>
            {purchaseUrl ? (
              <Button href={purchaseUrl} target="_blank" rel="noreferrer">
                {t('unlock.openInfo', { defaultValue: 'Open Unlock Info' })}
              </Button>
            ) : null}
          </CardBody>
        </Card>

        <Card className={[styles.card, styles.cardWide].join(' ')}>
          <CardBody className={styles.cardBody}>
            <p className={styles.cardEyebrow}>{t('unlock.grantsEyebrow', { defaultValue: 'Recorded Grants' })}</p>
            <h2 className={styles.cardTitle}>{t('unlock.grantsTitle', { defaultValue: 'Entitlement Sources' })}</h2>
            <div className={styles.grantList}>
              {grants.length > 0 ? grants.map((grant) => (
                <div key={`${grant.scope}-${grant.source}-${grant.grantedAt ?? 'now'}`} className={styles.grantItem}>
                  <div>
                    <div className={styles.grantScope}>{grant.scope}</div>
                    <div className={styles.grantMeta}>
                      {grant.source}
                      {grant.expiresAt ? ` • ${t('unlock.expiresAt', { defaultValue: 'expires' })} ${new Date(grant.expiresAt).toLocaleString()}` : ''}
                    </div>
                  </div>
                  <StatusChip
                    state={grant.active ? 'success' : 'warning'}
                    label={grant.active
                      ? t('unlock.grantActive', { defaultValue: 'active' })
                      : t('unlock.grantExpired', { defaultValue: 'expired' })}
                  />
                </div>
              )) : (
                <div className={styles.empty}>{t('unlock.noGrants', { defaultValue: 'No entitlement grants recorded yet.' })}</div>
              )}
            </div>
          </CardBody>
        </Card>
      </div>
    </div>
  );
}
