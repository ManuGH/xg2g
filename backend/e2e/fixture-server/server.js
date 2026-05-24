
const fastify = require('fastify')({ logger: true });
const fs = require('fs');
const path = require('path');
const cors = require('@fastify/cors');

// PR-7.0: Fixture Backend

const SCENARIOS_DIR = path.join(__dirname, '../scenarios');
const HLS_FIXTURE_DIR = path.join(__dirname, '../fixtures/hls');
const HLS_CONTENT_TYPES = {
  '.m3u8': 'application/vnd.apple.mpegurl',
  '.ts': 'video/mp2t',
};
let activeScenario = null;

// Register CORS to allow WebUI
fastify.register(cors, {
  origin: '*', // Strict: in real harness lock down to localhost
  methods: ['GET', 'POST', 'PUT', 'DELETE', 'OPTIONS']
});

// Load Scenario Helper
function loadScenario(id) {
  const file = path.join(SCENARIOS_DIR, `${id}.scenario.json`);
  if (!fs.existsSync(file)) return null;
  return JSON.parse(fs.readFileSync(file, 'utf-8'));
}

// Admin Endpoint to set scenario
fastify.post('/__admin/scenario', async (request, reply) => {
  const { id } = request.body;
  const scenario = loadScenario(id);
  if (!scenario) {
    reply.code(404).send({ error: 'Scenario not found' });
    return;
  }
  activeScenario = scenario;
  fastify.log.info(`Active Scenario set to: ${id}`);
  return { status: 'ok', active: id };
});

// Generic Handler
async function handleRequest(request, reply) {
  if (!activeScenario) {
    reply.code(500).send({ error: 'No active scenario' });
    return;
  }

  // Debug
  fastify.log.info({ path: request.url, method: request.method, activeScenario: activeScenario?.meta?.id }, 'Handle Request');

  // spec handling for capabilities which is separated in schema
  // NOTE: request.routerPath might be weird with wildcard match. Use request.url or check fastify routing.
  // In Fastify `request.url` is the full path. `request.routerPath` is the route pattern (e.g. /api/v3/*).

  // Check Capabilities specific path
  if (request.url.startsWith('/api/v3/system/capabilities') && request.method === 'GET') {
    return activeScenario.capabilities;
  }

  // Find matching endpoint. Exact (path + query string) wins; fall back to
  // bare-path entries only so query-specific scenario entries (e.g.
  // /services?bouquet=Favorites) are never incorrectly matched by a request
  // with a different query string. Bare-path entries are those defined without
  // a '?' in their path.
  const reqPathname = request.url.split('?')[0];
  const match =
    activeScenario.endpoints?.find(e => e.path === request.url && e.method === request.method) ||
    activeScenario.endpoints?.find(e => e.path && !e.path.includes('?') && e.path.split('?')[0] === reqPathname && e.method === request.method);

  if (match) {
    const { status, body, headers, delayMs } = match.response;
    if (delayMs) await new Promise(r => setTimeout(r, delayMs));
    if (headers) reply.headers(headers);
    reply.code(status).send(body);
    return;
  }

  // Default 404 for unknown mocked paths
  reply.code(404).send({ error: 'Mock path not defined in scenario' });
}

// HLS fixture: serve the generated playlist + segments for any session id.
// Matches the real backend path /api/v3/sessions/{id}/hls/{file} so the
// player's same-origin + /api/v3/ guard (useLiveSessionController.ts:206) holds.
// Registered before the wildcard; fastify resolves the specific route first.
// Also serves HEAD automatically (exposeHeadRoutes default), satisfying
// primePlaybackAuth's HEAD pre-flight.
fastify.get('/api/v3/sessions/:sessionId/hls/:filename', async (request, reply) => {
  const filename = path.basename(request.params.filename); // prevent traversal
  if (filename === '..' || filename === '.') {
    reply.code(400).send({ error: 'Invalid filename' });
    return;
  }
  const ext = path.extname(filename);
  const contentType = HLS_CONTENT_TYPES[ext];
  if (!contentType) {
    reply.code(404).send({ error: 'Unsupported HLS file type' });
    return;
  }
  const filePath = path.join(HLS_FIXTURE_DIR, filename);
  if (!fs.existsSync(filePath) || !fs.statSync(filePath).isFile()) {
    reply.code(404).send({ error: 'HLS fixture not found', filename });
    return;
  }
  reply.header('Content-Type', contentType);
  reply.header('Cache-Control', 'no-store');
  return reply.send(fs.createReadStream(filePath));
});

// Wildcard routes
fastify.get('/api/v3/*', handleRequest);
fastify.post('/api/v3/*', handleRequest);

const start = async () => {
  try {
    await fastify.listen({ port: 3001, host: '0.0.0.0' });
    console.log('Fixture Backend running on port 3001');
  } catch (err) {
    fastify.log.error(err);
    process.exit(1);
  }
};

start();
