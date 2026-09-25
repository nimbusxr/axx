package lsp

// The subset of the Language Server Protocol 3.17 that axx's server speaks.
// Positions are zero-based and count UTF-16 code units, the protocol's
// default encoding.

type position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type textRange struct {
	Start position `json:"start"`
	End   position `json:"end"`
}

// Diagnostic severities.
const (
	severityError   = 1
	severityWarning = 2
	severityInfo    = 3
)

type diagnostic struct {
	Range    textRange `json:"range"`
	Severity int       `json:"severity"`
	Code     string    `json:"code,omitempty"`
	Source   string    `json:"source"`
	Message  string    `json:"message"`
}

type publishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Version     *int         `json:"version,omitempty"`
	Diagnostics []diagnostic `json:"diagnostics"`
}

type textDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

type textDocumentIdentifier struct {
	URI string `json:"uri"`
}

type versionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

type didOpenParams struct {
	TextDocument textDocumentItem `json:"textDocument"`
}

type didChangeParams struct {
	TextDocument   versionedTextDocumentIdentifier `json:"textDocument"`
	ContentChanges []contentChange                 `json:"contentChanges"`
}

// contentChange is a full-text change: the server asks for full sync.
type contentChange struct {
	Text string `json:"text"`
}

type didCloseParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

type textDocumentPositionParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     position               `json:"position"`
}

type semanticTokensParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

type documentLinkParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
}

type documentLink struct {
	Range   textRange `json:"range"`
	Target  string    `json:"target"`
	Tooltip string    `json:"tooltip,omitempty"`
}

type markupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type hover struct {
	Contents markupContent `json:"contents"`
	Range    *textRange    `json:"range,omitempty"`
}

type textEdit struct {
	Range   textRange `json:"range"`
	NewText string    `json:"newText"`
}

// Completion item kinds and insert text formats.
const (
	completionKindSnippet = 15
	completionKindFile    = 17
	completionKindFolder  = 19
	insertFormatPlain     = 1
	insertFormatSnippet   = 2
)

type completionParams struct {
	TextDocument textDocumentIdentifier `json:"textDocument"`
	Position     position               `json:"position"`
	Context      struct {
		TriggerKind      int    `json:"triggerKind"`
		TriggerCharacter string `json:"triggerCharacter"`
	} `json:"context"`
}

// triggerCharacter is the completion trigger kind of a typed trigger
// character.
const triggerCharacter = 2

type initializeParams struct {
	Capabilities struct {
		Workspace struct {
			DidChangeWatchedFiles struct {
				DynamicRegistration bool `json:"dynamicRegistration"`
			} `json:"didChangeWatchedFiles"`
		} `json:"workspace"`
	} `json:"capabilities"`
}

type didChangeWatchedFilesParams struct {
	Changes []struct {
		URI string `json:"uri"`
	} `json:"changes"`
}

type completionItem struct {
	Label            string         `json:"label"`
	Kind             int            `json:"kind"`
	Detail           string         `json:"detail,omitempty"`
	Documentation    *markupContent `json:"documentation,omitempty"`
	FilterText       string         `json:"filterText"`
	SortText         string         `json:"sortText"`
	InsertTextFormat int            `json:"insertTextFormat"`
	TextEdit         textEdit       `json:"textEdit"`
}

type completionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []completionItem `json:"items"`
}

type semanticTokens struct {
	Data []uint32 `json:"data"`
}

// tokenTypes is the semantic token legend: step parameter values, and
// Scenario Outline <placeholders>.
var tokenTypes = []string{"parameter", "variable"}

const (
	tokenParameter = 0
	tokenVariable  = 1
)
