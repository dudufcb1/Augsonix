// Package codesearch is the semantic index over the workspace: it splits source
// files into chunks, embeds them through a provider, and answers natural-language
// queries by vector similarity followed by a rerank pass.
//
// It exists because grep only finds text you can already name. A model that has
// to guess identifiers burns turns on misses, and every miss replays another
// broad file span into the context window. Retrieval trades those variable
// searches for one call.
//
// The index is incremental and lives on disk under the workspace: file hashes
// decide what changed, and only changed files are re-embedded. Vectors are int8
// and searched by a flat scan, which keeps the whole store dependency-free —
// the binary ships with CGO disabled, so no C vector extension is available.
package codesearch
