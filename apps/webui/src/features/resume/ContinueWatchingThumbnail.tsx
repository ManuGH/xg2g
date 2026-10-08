import { useEffect, useRef, useState, type CSSProperties } from 'react';
import { resolveItemMonogram } from './normalizeResume';
import styles from './ContinueWatchingRail.module.css';

interface ContinueWatchingThumbnailProps {
  recordingId: string;
  title?: string;
  authToken?: string | null;
}

const thumbnailMissCache = new Set<string>();

const PREVIEW_ACCENTS = [
  'var(--accent-action)',
  'var(--accent-live)',
  'var(--status-info)',
  'var(--status-success)',
] as const;

export default function ContinueWatchingThumbnail({
  recordingId,
  title,
  authToken,
}: ContinueWatchingThumbnailProps) {
  const thumbnailUrl = recordingId
    ? `/api/v3/recordings/${encodeURIComponent(recordingId)}/thumbnail.jpg`
    : null;

  const objectUrlRef = useRef<string | null>(null);
  const [resolvedThumbnailUrl, setResolvedThumbnailUrl] = useState<string | null>(null);
  const [thumbnailUnavailable, setThumbnailUnavailable] = useState<boolean>(() => {
    if (!thumbnailUrl) return true;
    return thumbnailMissCache.has(thumbnailUrl);
  });

  useEffect(() => {
    if (!thumbnailUrl || thumbnailMissCache.has(thumbnailUrl)) {
      setResolvedThumbnailUrl(null);
      setThumbnailUnavailable(true);
      return;
    }

    const controller = new AbortController();
    const headers: Record<string, string> = {};
    const normalizedToken = String(authToken || '').trim();
    if (normalizedToken) {
      headers.Authorization = `Bearer ${normalizedToken}`;
    }

    void fetch(thumbnailUrl, {
      method: 'GET',
      credentials: 'same-origin',
      headers,
      signal: controller.signal,
    })
      .then(async (response) => {
        if (!response.ok) {
          throw new Error(`thumbnail ${response.status}`);
        }
        return response.blob();
      })
      .then((blob) => {
        if (controller.signal.aborted) return;
        const objectUrl = URL.createObjectURL(blob);
        objectUrlRef.current = objectUrl;
        setResolvedThumbnailUrl(objectUrl);
        setThumbnailUnavailable(false);
      })
      .catch(() => {
        if (controller.signal.aborted) return;
        thumbnailMissCache.add(thumbnailUrl);
        setResolvedThumbnailUrl(null);
        setThumbnailUnavailable(true);
      });

    return () => {
      controller.abort();
      if (objectUrlRef.current) {
        URL.revokeObjectURL(objectUrlRef.current);
        objectUrlRef.current = null;
        setResolvedThumbnailUrl(null);
      }
    };
  }, [authToken, thumbnailUrl]);

  // Determine an accent color deterministically from recordingId / title
  const seed = `${recordingId}:${title || ''}`;
  let hash = 0;
  for (let i = 0; i < seed.length; i += 1) {
    hash = (hash * 31 + seed.charCodeAt(i)) >>> 0;
  }
  const accent = PREVIEW_ACCENTS[hash % PREVIEW_ACCENTS.length];

  return (
    <div
      className={styles.thumbnailContainer}
      style={{ '--card-accent': accent } as CSSProperties}
    >
      {resolvedThumbnailUrl && !thumbnailUnavailable ? (
        <img
          className={styles.thumbnailImage}
          src={resolvedThumbnailUrl}
          alt=""
          loading="lazy"
          decoding="async"
          draggable={false}
        />
      ) : (
        <div className={styles.thumbnailFallback} aria-hidden="true">
          <span className={styles.fallbackWordmark}>{resolveItemMonogram(title)}</span>
        </div>
      )}
    </div>
  );
}
