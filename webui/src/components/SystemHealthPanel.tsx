// System Health Panel - Now using Card + StatusChip primitives
// CTO Contract: No custom CSS, uses primitives only


import { Card } from './ui/Card';
import { StatusChip, type ChipState } from './ui/StatusChip';
import styles from './SystemHealthPanel.module.css';

export interface HealthTileData {
  label: string;
  value: string | number;
  status: ChipState;
}

export interface SystemHealthPanelProps {
  tiles: HealthTileData[];
}

export function SystemHealthPanel({ tiles }: SystemHealthPanelProps) {
  return (
    <div className={styles.grid} role="status" aria-label="System Health">
      {tiles.map((tile, index) => (
        <Card
          key={index}
          variant={tile.status === 'live' || tile.status === 'recording' ? 'live' : 'standard'}
          className={styles.tile}
        >
          <div className={styles.tileContent}>
            <div className={styles.tileLabel}>{tile.label}</div>
            <div className={`${styles.tileValue} tabular`.trim()}>{tile.value}</div>
            {tile.status !== 'idle' && (
              <StatusChip state={tile.status} label="" showIcon={true} />
            )}
          </div>
        </Card>
      ))}
    </div>
  );
}

// Example usage for Dashboard
export function ExampleHealthPanel() {
  const tiles: HealthTileData[] = [
    { label: 'Receiver', value: '✓', status: 'success' },
    { label: 'EPG', value: '✓', status: 'success' },
    { label: 'Streams', value: 2, status: 'live' },
    { label: 'Rec', value: '●', status: 'recording' },
    { label: 'Uptime', value: '14h', status: 'idle' }
  ];

  return <SystemHealthPanel tiles={tiles} />;
}
