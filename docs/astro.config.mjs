// @ts-check
import starlight from '@astrojs/starlight';
import { ExpressiveCodeTheme } from '@astrojs/starlight/expressive-code';
import { defineConfig } from 'astro/config';
import starlightLinksValidator from 'starlight-links-validator';
import starlightLlmsTxt from 'starlight-llms-txt';
import { axxConsole } from './src/lib/axx-console.mjs';
import { poseEditor } from './scripts/pose-editor.mjs';

const site = 'https://axx.nimbusxr.us';

export default defineConfig({
	site,
	trailingSlash: 'ignore',
	vite: { plugins: [poseEditor()] },
	integrations: [
		starlight({
			title: 'axx',
			description:
				'Axx ("axxeptance") is a human-readable acceptance testing framework for the agentic era: acceptance criteria people can read, run black-box against services with REST + OpenAPI, WireMock, SQL, MongoDB and Kafka, and extensible with Go packs.',
			favicon: '/favicon.svg',
			social: [{ icon: 'github', label: 'GitHub', href: 'https://github.com/nimbusxr/axx' }],
			editLink: { baseUrl: 'https://github.com/nimbusxr/axx/edit/main/docs/' },
			customCss: ['./src/styles/custom.css'],
			components: {
				Header: './src/components/Header.astro',
				Hero: './src/components/Hero.astro',
				Footer: './src/components/Footer.astro',
				PageTitle: './src/components/PageTitle.astro',
				ThemeProvider: './src/components/ThemeProvider.astro',
				ThemeSelect: './src/components/ThemeSelect.astro',
			},
			expressiveCode: {
				useDarkModeMediaQuery: false,
				shiki: {
					langs: [axxConsole],
					langAlias: { console: 'axx-console' },
				},
				themes: [new ExpressiveCodeTheme({
					name: 'axx-obsidian',
					type: 'dark',
					colors: {
						'editor.background': '#0b0b0c',
						'editor.foreground': '#a7a7ae',
						'editor.selectionBackground': '#29292f',
						'editorError.foreground': '#e48a8a',
						'editorWarning.foreground': '#e0b267',
						'editorInfo.foreground': '#78b7ed',
						'terminal.ansiRed': '#e48a8a',
						'terminal.ansiGreen': '#7bc792',
						'terminal.ansiYellow': '#e0b267',
						'terminal.ansiBlue': '#78b7ed',
						'terminal.ansiMagenta': '#bc9aeb',
						'terminal.ansiCyan': '#67c5ba',
						'terminal.ansiBlack': '#29292c',
						'terminal.ansiWhite': '#a7a7ae',
						'terminal.ansiBrightBlack': '#75757e',
						'terminal.ansiBrightRed': '#eba2a2',
						'terminal.ansiBrightGreen': '#98d4a9',
						'terminal.ansiBrightYellow': '#edc385',
						'terminal.ansiBrightBlue': '#96c5ee',
						'terminal.ansiBrightMagenta': '#cbb0f2',
						'terminal.ansiBrightCyan': '#8ed8ce',
						'terminal.ansiBrightWhite': '#b8b8bd',
					},
					tokenColors: [
						{ scope: ['comment'], settings: { foreground: '#75757e' } },
						{ scope: ['keyword', 'storage'], settings: { foreground: '#bc9aeb' } },
						{ scope: ['string'], settings: { foreground: '#a4c879' } },
						{ scope: ['constant'], settings: { foreground: '#e0b267' } },
						{ scope: ['entity', 'support.function'], settings: { foreground: '#78b7ed' } },
						{ scope: ['entity.name.type', 'support.type', 'support.class'], settings: { foreground: '#67c5ba' } },
						{ scope: ['variable.parameter', 'variable.other.constant'], settings: { foreground: '#dba071' } },
						{ scope: ['support.type.property-name', 'entity.name.tag', 'string.unquoted.plain.out.yaml'], settings: { foreground: '#78b7ed' } },
						{ scope: ['punctuation'], settings: { foreground: '#85858d' } },
						{ scope: ['markup.inserted'], settings: { foreground: '#7bc792' } },
						{ scope: ['markup.deleted', 'invalid'], settings: { foreground: '#e48a8a' } },
						{ scope: ['markup.warning'], settings: { foreground: '#e0b267' } },
					],
				}), new ExpressiveCodeTheme({
					name: 'axx-graphite',
					type: 'light',
					colors: {
						'editor.background': '#d4d4d8',
						'editor.foreground': '#34343b',
						'editor.selectionBackground': '#bdbdc6',
						'editorError.foreground': '#981c34',
						'editorWarning.foreground': '#774100',
						'editorInfo.foreground': '#004ca4',
						'terminal.ansiRed': '#981c34',
						'terminal.ansiGreen': '#005b29',
						'terminal.ansiYellow': '#774100',
						'terminal.ansiBlue': '#004ca4',
						'terminal.ansiMagenta': '#6f23b0',
						'terminal.ansiCyan': '#005955',
						'terminal.ansiBlack': '#b9b9c0',
						'terminal.ansiWhite': '#34343b',
						'terminal.ansiBrightBlack': '#5c5c66',
						'terminal.ansiBrightRed': '#88162d',
						'terminal.ansiBrightGreen': '#005125',
						'terminal.ansiBrightYellow': '#693900',
						'terminal.ansiBrightBlue': '#004294',
						'terminal.ansiBrightMagenta': '#641ba1',
						'terminal.ansiBrightCyan': '#004e4b',
						'terminal.ansiBrightWhite': '#29292f',
					},
					tokenColors: [
						{ scope: ['comment'], settings: { foreground: '#5c5c66' } },
						{ scope: ['keyword', 'storage'], settings: { foreground: '#6f23b0' } },
						{ scope: ['string'], settings: { foreground: '#2d580b' } },
						{ scope: ['constant'], settings: { foreground: '#774100' } },
						{ scope: ['entity', 'support.function'], settings: { foreground: '#004ca4' } },
						{ scope: ['entity.name.type', 'support.type', 'support.class'], settings: { foreground: '#005955' } },
						{ scope: ['variable.parameter', 'variable.other.constant'], settings: { foreground: '#893506' } },
						{ scope: ['support.type.property-name', 'entity.name.tag', 'string.unquoted.plain.out.yaml'], settings: { foreground: '#004ca4' } },
						{ scope: ['punctuation'], settings: { foreground: '#62626c' } },
						{ scope: ['markup.inserted'], settings: { foreground: '#005b29' } },
						{ scope: ['markup.deleted', 'invalid'], settings: { foreground: '#981c34' } },
						{ scope: ['markup.warning'], settings: { foreground: '#774100' } },
					],
				})],
				styleOverrides: {
					codeBackground: ['#0b0b0c', '#d4d4d8'],
					borderColor: ['#262628', '#b9b9c0'],
					borderRadius: '0.75rem',
					codeFontSize: '0.8125rem',
					textMarkers: {
						markBackground: ['#78b7ed14', '#004ca422'],
						markBorderColor: ['#78b7ed55', '#004ca477'],
						insBackground: ['#7bc79214', '#005b2922'],
						insBorderColor: ['#7bc79255', '#005b2977'],
						insDiffIndicatorColor: ['#7bc792', '#005b29'],
						delBackground: ['#e48a8a14', '#981c3422'],
						delBorderColor: ['#e48a8a55', '#981c3477'],
						delDiffIndicatorColor: ['#e48a8a', '#981c34'],
					},
					frames: {
						editorActiveTabBackground: ['#141415', '#ceced3'],
						editorActiveTabForeground: ['#aaaab1', '#424249'],
						editorTabBarBackground: ['#101011', '#c9c9cf'],
						terminalBackground: ['#0b0b0c', '#d4d4d8'],
						terminalTitlebarBackground: ['#141415', '#ceced3'],
						terminalTitlebarForeground: ['#aaaab1', '#424249'],
						tooltipSuccessBackground: ['#131c17', '#c3dec9'],
						tooltipSuccessForeground: ['#98d4a9', '#005125'],
					},
				},
			},
			routeMiddleware: './src/route-data.ts',
			head: [
				{ tag: 'meta', attrs: { name: 'theme-color', content: '#080808' } },
				{ tag: 'meta', attrs: { name: 'color-scheme', content: 'dark light' } },
				{ tag: 'link', attrs: { rel: 'preload', href: '/fonts/inter-latin.woff', as: 'font', type: 'font/woff', crossorigin: 'anonymous' } },
				{
					tag: 'link',
					attrs: { rel: 'alternate', type: 'text/plain', title: 'llms.txt', href: '/llms.txt' },
				},
			],
			sidebar: [
				{
					label: 'Tutorials',
					items: ['tutorials/quickstart', 'tutorials/first-suite', 'tutorials/with-an-agent'],
				},
				{
					label: 'Guides',
					items: [
						{ label: 'Set up', items: ['guides/install', 'guides/set-up-your-editor', 'guides/configure-services', 'guides/manage-app-lifecycle', 'guides/run-in-ci', 'guides/set-up-agents'] },
						{ label: 'Test', items: ['guides/validate-openapi', 'guides/mock-dependencies', 'guides/seed-and-query-sql', 'guides/seed-mongodb', 'guides/test-kafka-avro', 'guides/check-logs', 'guides/test-cloud-services', 'guides/use-packs', 'guides/write-custom-steps'] },
						{ label: 'Test data', items: ['guides/fixture-factories', 'guides/isolate-test-data'] },
						{ label: 'Run and diagnose', items: ['guides/parallel-runs', 'guides/tags-and-filtering', 'guides/reports', 'guides/debug-failures'] },
					],
				},
				{
					label: 'References',
					items: [
						// Steps, the command line and the codes are generated (scripts/gen.mjs).
						// Each command's page is linked from the command line overview.
						{
							label: 'Steps',
							items: [
								'references/steps',
								'references/steps/rest',
								'references/steps/mock',
								'references/steps/sql',
								'references/steps/mongo',
								'references/steps/kafka',
								'references/steps/logs',
								// The cloud service packs, by cloud.
								{
									label: 'AWS',
									collapsed: true,
									items: [
										'references/steps/aws-core',
										'references/steps/aws-s3',
										'references/steps/aws-sqs',
										'references/steps/aws-sns',
										'references/steps/aws-eventbridge',
										'references/steps/aws-dynamodb',
									],
								},
								{
									label: 'Google Cloud',
									collapsed: true,
									items: [
										'references/steps/gcp-core',
										'references/steps/gcp-storage',
										'references/steps/gcp-pubsub',
										'references/steps/gcp-bigquery',
										'references/steps/gcp-firestore',
									],
								},
								{
									label: 'Azure',
									collapsed: true,
									items: ['references/steps/azure-blob', 'references/steps/azure-servicebus'],
								},
								'references/step-index',
							],
						},
						'references/config',
						{ label: 'Command line', slug: 'references/cli' },
						'references/json-output',
						'references/error-codes',
					],
				},
				{
					label: 'Explanations',
					items: [
						'explanations/why-axx',
						'explanations/how-axx-works',
						'explanations/black-box-testing',
						'explanations/scenario-isolation',
						'explanations/step-design',
						'explanations/openapi-contract',
						'explanations/axx-for-agents',
						'explanations/faq',
						'explanations/roadmap',
						'explanations/adrs',
					],
				},
			],
			plugins: [
				starlightLinksValidator({
					// Files served from public/ (schemas, skills, llms.txt) are not pages.
					exclude: ['/schemas/**', '/skills/**', '/.well-known/**', '/llms.txt', '/llms-full.txt', '/llms-small.txt', '/**/*.md', '/index.md'],
				}),
				starlightLlmsTxt({
					projectName: 'Axx',
					description:
						'Axx (github.com/nimbusxr/axx, "axxeptance") is a human-readable acceptance testing framework for the agentic era.',
					details: [
						'- Every page is also available as Markdown: append `.md` to its path (for example `https://axx.nimbusxr.us/tutorials/quickstart.md`).',
						'- Never invent step text. Search the step index (`/references/step-index/`) or run `axx steps search "<intent>"`, then validate with `axx validate`.',
						'- Agent skills: `/.well-known/agent-skills/index.json`. JSON Schema for axx.yaml: `/schemas/v0/axx.schema.json`.',
						'- Axx is pre-release (v0.x): interfaces may change before v1.0.0.',
					].join('\n'),
					customSets: [
						{ label: 'Tutorials', paths: ['tutorials/**'], description: 'quickstart, a first suite, and testing with a coding agent' },
						{ label: 'Guides', paths: ['guides/**'], description: 'task guides: services, apps, CI, OpenAPI, mocks, SQL, MongoDB, Kafka, logs, cloud services (AWS, Google Cloud, Azure), test data, packs, custom steps, debugging, agents' },
						{ label: 'References', paths: ['references/**'], description: 'generated step, CLI and error-code reference, axx.yaml, JSON output and exit codes' },
						{ label: 'Explanations', paths: ['explanations/**'], description: 'why Axx, how it works: black-box testing, isolation, step design, OpenAPI, agents' },
					],
					promote: ['index*', 'explanations/why-axx', 'tutorials/**'],
					demote: ['explanations/{faq,roadmap,adrs}'],
					exclude: ['references/cli/**', 'references/error-codes', 'explanations/{roadmap,adrs}'],
				}),
			],
		}),
	],
});
