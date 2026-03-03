
const fastify = require('fastify')({ logger: true });
const fs = require('fs');
const path = require('path');
const cors = require('@fastify/cors');

// PR-7.0: Fixture Backend

const SCENARIOS_DIR = path.join(__dirname, '../scenarios');
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

  // Find matching endpoint
  const match = activeScenario.endpoints.find(e =>
    e.path === request.url && e.method === request.method
  );

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
