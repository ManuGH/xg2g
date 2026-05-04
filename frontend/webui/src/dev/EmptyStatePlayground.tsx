// Dev-only playground for the EmptyState primitive.
// Mounted only when import.meta.env.DEV is true (see App.tsx).

import { Button, EmptyState } from '../components/ui';

const wrap: React.CSSProperties = {
  position: 'fixed',
  inset: 0,
  overflowY: 'auto',
  padding: 24,
  background: 'var(--surface-stage, #0a0a0a)',
  color: 'var(--text-primary)',
  fontFamily: 'var(--font-body)',
  zIndex: 9999,
};

const inner: React.CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: 24,
  maxWidth: 960,
  margin: '0 auto',
};

const sectionLabel: React.CSSProperties = {
  fontFamily: 'var(--font-mono)',
  fontSize: 'var(--text-xs)',
  color: 'var(--text-tertiary)',
  textTransform: 'uppercase',
  letterSpacing: '0.12em',
};

export default function EmptyStatePlayground() {
  return (
    <div style={wrap}>
      <div style={inner}>
      <h1 style={{ fontFamily: 'var(--font-heading)', margin: 0 }}>EmptyState Playground</h1>

      <section>
        <p style={sectionLabel}>variant=panel · Timers (no data)</p>
        <EmptyState
          icon="○"
          title="No timers scheduled."
          description="Create a timer from the EPG or recordings to schedule a recording."
        />
      </section>

      <section>
        <p style={sectionLabel}>variant=panel · Profile blocked</p>
        <EmptyState icon="⛔" title="Dieses Profil darf den DVR nicht bedienen." />
      </section>

      <section>
        <p style={sectionLabel}>variant=panel · Recordings empty location</p>
        <EmptyState icon="○" title="No recordings in this location yet." />
      </section>

      <section>
        <p style={sectionLabel}>variant=panel · with action</p>
        <EmptyState
          icon="○"
          title="Nothing here yet"
          description="Create your first item to get started."
          action={<Button variant="primary">Create item</Button>}
        />
      </section>

      <section>
        <p style={sectionLabel}>variant=inline · Logs</p>
        <EmptyState variant="inline" icon="○" title="No logs available." />
      </section>

      <section>
        <p style={sectionLabel}>variant=panel · long description wrapping</p>
        <EmptyState
          icon="○"
          title="Long-form empty state"
          description="This description is intentionally a longer sentence that should wrap on small screens to verify the max-width constraint and keeps reading comfortable in mobile portrait."
        />
      </section>
      </div>
    </div>
  );
}
