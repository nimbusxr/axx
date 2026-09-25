/** Semantic highlighting for documented axx output; the copied text stays untouched. */
export const axxConsole = {
	name: 'axx-console',
	scopeName: 'text.axx-console',
	patterns: [
		{ match: '^\\s*\\$', name: 'entity.name.function' },
		{ match: '^\\s*[✓+]\\s', name: 'markup.inserted' },
		{ match: '^\\s*[✗x]\\s|^Failed scenarios:', name: 'markup.deleted' },
		{ match: '\\b0 (?:failed|failures?|errors?|warnings?)\\b', name: 'comment' },
		{ match: '\\b[1-9][0-9]* (?:failed|failures?|errors?)\\b', name: 'markup.deleted' },
		{ match: '\\b[0-9]+ passed\\b|\\bok\\s*$', name: 'markup.inserted' },
		{ match: '\\b[1-9][0-9]* (?:warnings?|skipped|pending|undefined|ambiguous)\\b', name: 'markup.warning' },
		{ match: '^\\s*(?:error|ERROR|FAIL|failed|actual):|\\bAXE-E[0-9]{4}\\b', name: 'markup.deleted' },
		{ match: '^\\s*(?:warn|WARN|warning|WARNING|undefined|pending):', name: 'markup.warning' },
		{ match: '^\\s*(?:expected|create):?', name: 'markup.inserted' },
		{ match: '^\\s*(?:Feature|Scenario|Background):', name: 'keyword' },
		{ match: '^\\s*(?:axx|log|attachment|rerun|next|info|INFO):', name: 'entity.name.function' },
		{ match: '#\\s+features/.*$', name: 'comment' },
	],
};
