export const TEMPLATES = [
  { name: 'Blank', text: '' },
  { name: 'Bug report', text: 'Bug: \nRoot cause: \nFix applied: \nPrevention: ' },
  { name: 'Lesson learned', text: "Lesson: \nContext: \nDo: \nDon't: " },
  { name: 'API usage', text: 'API: \nEndpoint: \nAuth: \nExample: \nGotcha: ' },
  { name: 'Architecture note', text: 'Component: \nResponsibility: \nInterface: \nTrade-off: ' },
  { name: 'Workflow tip', text: 'Tool: \nTip: \nWhy it helps: \nExample: ' },
];

export function getTemplate(name) {
  return TEMPLATES.find(t => t.name === name) ?? TEMPLATES[0];
}
