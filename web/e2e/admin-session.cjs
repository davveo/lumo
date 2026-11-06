// The isolated browser suite shares a session instead of repeatedly hitting
// the real per-account login limiter. The runner owns and removes this file.
const fs = require('node:fs');
let cached = '';
async function adminToken(request) {
  if (cached) return cached;
  const file = process.env.LUMO_E2E_ADMIN_SESSION_FILE;
  if (file && fs.existsSync(file)) return (cached = fs.readFileSync(file, 'utf8'));
  const response = await request.post('/api/v1/iam/login', {
    data: { data: { username: 'admin', password: 'Admin@2026' } },
  });
  if (!response.ok()) throw new Error('Isolated admin session could not be created: ' + response.status());
  cached = (await response.json()).data.token;
  if (file) fs.writeFileSync(file, cached, { mode: 0o600 });
  return cached;
}
module.exports = { adminToken };
