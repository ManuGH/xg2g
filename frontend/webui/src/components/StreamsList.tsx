// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { useStreams } from '../hooks/useServerQueries';
import { Card, CardHeader, CardBody, StatusChip } from './ui';
import type { StreamSession } from '../client-ts';
import styles from './Streams.module.css';

interface StreamsListProps {
  compact?: boolean;
}


function assertNever(x: never): never {
  throw new Error(`Unhandled StreamSessionState: ${String(x)}`);
}

/**
 * mapStreamToChip
 * Strictly follows Domain Truth Mapping (CTO Hardrail) for P3-2.
 * Maps exact OpenAPI enum states to UI semantics.
 */
function mapStreamToChip(session: StreamSession) {
  switch (session.state) { // xg2g:allow-webui-logic
    case 'starting':
      return { state: 'idle', label: 'STARTING' } as const;
    case 'buffering':
      return { state: 'warning', label: 'BUFFERING' } as const;
    case 'active':
      // Pulse reserved for live/recording; active simply means "healthy pipeline"
      return { state: 'success', label: 'ACTIVE' } as const;
    case 'stalled':
      // Operator-critical warning
      return { state: 'warning', label: 'STALLED' } as const;
    case 'ending':
      return { state: 'idle', label: 'ENDING' } as const;
    case 'idle':
      return { state: 'idle', label: 'IDLE' } as const;
    case 'error':
      return { state: 'error', label: 'ERROR', pulse: true } as const;
  }
  // Fail-closed for unknown states (runtime safety + strict exhaustiveness)
  return assertNever(session.state);
}

const maskIP = (ip: string | undefined): string => {
  if (!ip) return '';
  return ip.replace(/\.\d+$/, '.xxx');
};

const formatDuration = (date: Date): string => {
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  const diffMins = Math.floor(diffMs / 60000);
  if (diffMins < 60) return `${diffMins}m`;
  const diffHours = Math.floor(diffMins / 60);
  return `${diffHours}h ${diffMins % 60}m`;
};

export default function StreamsList({ compact = false }: StreamsListProps) {
  const { data: streams = [], error } = useStreams();
  const count = streams.length;

  if (count === 0 && !error) return null;

  return (
    <div className={[styles.section, compact ? styles.sectionCompact : null].filter(Boolean).join(' ')}>
      {!compact && (
        <h3 className={styles.heading}>
          Active Streams <span className="tabular">({count})</span>
        </h3>
      )}
      {error && <p className={styles.errorText}>Failed to load stream details</p>}

      <div className={styles.grid}>
        {streams.map((s: StreamSession) => {
          const chip = mapStreamToChip(s);
          return (
            <Card key={s.id} variant="standard" className={styles.streamCard} interactive>
              <CardHeader>
                <div className={styles.streamHeader}>
                  <div className={styles.streamTitleGroup}>
                    <StatusChip state="success" label="STREAM" showIcon={false} />
                    <div className={styles.streamChannel}>{s.channelName || 'Unknown Channel'}</div>
                  </div>
                  <StatusChip state={chip.state} label={chip.label} />
                </div>
              </CardHeader>

              <CardBody>
                {s.program?.title && (
                  <div className={styles.streamProgram}>
                    <div className={styles.programTitle}>{s.program.title}</div>
                    <div className={styles.programDesc}>{s.program.description}</div>
                  </div>
                )}
                <div className={styles.streamMeta}>
                  <div className={styles.metaItem}>
                    <span className={styles.metaLabel}>Client:</span>
                    <span className="tabular">{maskIP(s.clientIp)}</span>
                  </div>
                  <div className={styles.metaItem}>
                    <span className={styles.metaLabel}>Started:</span>
                    <span className="tabular">{s.startedAt ? formatDuration(new Date(s.startedAt)) : 'unknown'}</span>
                  </div>
                </div>
              </CardBody>
            </Card>
          );
        })}
      </div>
    </div>
  );
}
