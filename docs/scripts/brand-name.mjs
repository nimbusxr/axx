/** Capitalize the product in Markdown prose without changing executable examples or URLs. */
export function capitalizeDocsBrand(markdown, commandNames = []) {
	const commands = new Set([...commandNames, 'help']);
	const capitalize = (prose) => prose.replace(/(?<![\w./@-])axx(?![\w/-]|\.[\w])/g, (word, offset) => {
		const next = prose.slice(offset + word.length).match(/^[ \t]+([a-z][a-z-]*|--?[\w-]+)\b/);
		return next && (commands.has(next[1]) || next[1].startsWith('-')) ? word : 'Axx';
	});
	// Keep fenced/inline code, link destinations, URLs, and maintainer comments verbatim.
	const protectedSyntax = /(?<fence>^[ \t]*(?<marker>`{3,}|~{3,})[^\n]*\n[\s\S]*?^[ \t]*\k<marker>[ \t]*$)|(?<ticks>`+)[^`]*\k<ticks>|\]\([^\n)]*\)|https?:\/\/[^\s<>]+|<!--[\s\S]*?-->/gm;
	let result = '', position = 0;
	for (const match of markdown.matchAll(protectedSyntax)) {
		result += capitalize(markdown.slice(position, match.index)) + match[0];
		position = match.index + match[0].length;
	}
	return result + capitalize(markdown.slice(position));
}
