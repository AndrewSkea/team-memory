export async function postToSlack(webhookUrl, text, fetchFn = fetch) {
  const r = await fetchFn(webhookUrl, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ text }),
  });
  if (!r.ok) throw new Error(`Slack post failed: ${r.status}`);
}
