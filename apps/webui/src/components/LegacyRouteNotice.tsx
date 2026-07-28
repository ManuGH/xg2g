import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { Button } from './ui';
import styles from './LegacyRouteNotice.module.css';

interface LegacyRouteNoticeProps {
  parentLabel: string;
  description: string;
  route: string;
}

export default function LegacyRouteNotice({
  parentLabel,
  description,
  route,
}: LegacyRouteNoticeProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();

  return (
    <section className={styles.notice}>
      <div className={styles.copy}>
        <p className={styles.eyebrow}>
          {t('legacyRoute.eyebrow', { defaultValue: 'Expert path' })}
        </p>
        <p className={styles.title}>
          {t('legacyRoute.title', {
            defaultValue: 'This page now lives under {{parent}}',
            parent: parentLabel,
          })}
        </p>
        <p className={styles.description}>{description}</p>
      </div>
      <Button
        variant="secondary"
        size="sm"
        onClick={() => navigate(route)}
      >
        {t('legacyRoute.openParent', {
          defaultValue: 'Open {{parent}}',
          parent: parentLabel,
        })}
      </Button>
    </section>
  );
}
