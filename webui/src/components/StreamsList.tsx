// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

import { useStreams } from '../hooks/useServerQueries';
import { Card, CardHeader, CardBody, StatusChip } from './ui';
import type { StreamSession } from '../client-ts';
import './Streams.css';

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
  switch (session.state) {
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
    <div className={`streams-section ${compact ? 'streams-section--compact' : ''}`}>
      {!compact && <h3>Active Streams <span className="tabular">({count})</span></h3>}
      {error && <p className="error-text">Failed to load stream details</p>}

      <div className="streams-grid">
        {streams.map((s: StreamSession) => {
          const chip = mapStreamToChip(s);
          return (
            <Card key={s.id} variant="standard" className="stream-card" interactive>
              <CardHeader>
                <div className="stream-header">
                  <div className="stream-title-group">
                    <StatusChip state="success" label="STREAM" showIcon={false} />
                    <div className="stream-channel">{s.channel_name || 'Unknown Channel'}</div>
                  </div>
                  <StatusChip state={chip.state} label={chip.label} />
                </div>
              </CardHeader>

              <CardBody>
                {s.program?.title && (
                  <div className="stream-program">
                    <div className="program-title">{s.program.title}</div>
                    <div className="program-desc">{s.program.description}</div>
                  </div>
                )}
                <div className="stream-meta">
                  <div className="meta-item">
                    <span className="meta-label">Client:</span>
                    <span className="tabular">{maskIP(s.client_ip)}</span>
                  </div>
                  <div className="meta-item">
                    <span className="meta-label">Started:</span>
                    <span className="tabular">{s.started_at ? formatDuration(new Date(s.started_at)) : 'unknown'}</span>
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
