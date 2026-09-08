# Tools

## Overview

Tools give agents the ability to execute actions. The `Tool` interface embeds `permission.Checker` for fine-grained access control.

## Built-in Tools

| Tool | Description | Safety |
|------|-------------|--------|
| `Bash` | Execute shell commands | AST-level injection detection, dangerous path protection, read-only command recognition |
| `Read` | Read files with line range support | Path validation, line truncation (>2000 chars) |
| `Write` | Create/overwrite files | Generates unified diff in response metadata |
| `Edit` | Search/replace in files | Generates unified diff in response metadata |
| `Glob` | File pattern matching | Read-only |
| `Grep` | Text search with regex | Read-only |
| `ResetTools` | Activate/deactivate tool groups | Meta-tool |
| `TaskCreate` | Create tasks with dependencies | Bidirectional blocks/blockedBy |
| `TaskGet` | Get task details | Read-only |
| `TaskList` | List all tasks | Read-only |
| `TaskUpdate` | Update task status/fields | Dependency tracking |

Use `tool.NewEnhancedToolkit()` to get all built-in tools, or select individually:

```go
tk := tool.NewToolkit(tool.BashTool(), tool.ReadTool(), tool.WriteTool())
```

### Bash Tool Options

```go
tool.BashTool(
    tool.WithCwd("/path/to/workdir"),  // set working directory
)
```

## Custom Function Tools

Wrap any Go function as a tool:

```go
weatherTool := tool.NewFunctionTool(
    "get_weather",
    "Get current weather for a city",
    json.RawMessage(`{
        "type": "object",
        "properties": {
            "city": {"type": "string", "description": "City name"}
        },
        "required": ["city"]
    }`),
    func(ctx context.Context, input map[string]any) (any, error) {
        city, _ := input["city"].(string)
        return map[string]any{"city": city, "temp": "22°C"}, nil
    },
)
```

## Tool Groups

Organize tools into activatable groups:

```go
tk := tool.NewToolkit(weatherTool, searchTool, calcTool)
tk.AddGroup("research", searchTool, calcTool)
tk.ActivateGroup("research")   // only research tools available
tk.DeactivateGroup("research")  // restore defaults
```

The `ResetTools` meta-tool lets agents manage groups themselves.

## MCP Tools

Discover and use remote tools via Model Context Protocol:

```go
client, _ := mcp.NewHttpClient(ctx, &mcp.HttpConfig{URL: "http://mcp-server:8080"})
mcpToolkit, _ := mcp.NewMCPToolkit(ctx, client)
merged := mcp.MergeToolkits(mcpToolkit, localToolkit)
```

## Document Parsers (RAG)

The `rag/parser` package converts common file formats into `rag.Document` slices ready for indexing. Each parser supports configurable text chunking with overlap.

### Supported Formats

| Parser | Extensions | Description |
|--------|-----------|-------------|
| `TextParser` | `.txt`, `.md`, `.csv`, `.log` | Plain text with configurable chunking |
| `PDFParser` | `.pdf` | Stream-based text extraction (FlateDecode + plain text) |
| `WordParser` | `.docx` | XML-based text extraction from Word documents |
| `ExcelParser` | `.xlsx` | Row-based text extraction from spreadsheets |
| `PPTParser` | `.pptx` | Slide text extraction from PowerPoint |

### Usage

```go
import "github.com/alanfokco/agentscope-go/v2/pkg/agentscope/rag/parser"

// Parse a PDF into document chunks
p := &parser.PDFParser{Cfg: parser.DefaultChunkConfig()}
f, _ := os.Open("report.pdf")
docs, _ := p.Parse(ctx, f, "report.pdf")

// Each doc has Content (text) and Meta (source, page, etc.)
for _, doc := range docs {
    fmt.Printf("Chunk: %s... (source: %s)\n", doc.Content[:80], doc.Meta["source"])
}
```

### Chunk Configuration

Control how text is split into chunks:

```go
cfg := parser.ChunkConfig{
    MaxChunkSize: 1000,  // max characters per chunk
    Overlap:      200,   // overlap between consecutive chunks
}

p := &parser.TextParser{Cfg: cfg}
```

### Integration with RAG Pipeline

Parse documents and add to a knowledge base:

```go
// Parse
p := &parser.WordParser{Cfg: parser.DefaultChunkConfig()}
docs, _ := p.Parse(ctx, file, "manual.docx")

// Index
index := rag.NewInMemoryIndex()
index.AddDocuments(ctx, docs)

// Query
results, _ := index.Query(ctx, "installation steps", 5)
```

## Vector Store Backends

The `rag` package supports multiple vector store backends for document retrieval:

| Backend | Description | Use Case |
|---------|-------------|----------|
| `InMemoryIndex` | Simple linear scan, no external dependencies | Prototyping, small datasets |
| `QdrantIndex` | Qdrant vector database backend | Production, large-scale retrieval |
| `QdrantTextIndex` | Auto-embeds text then stores in Qdrant | Production with automatic embedding |

### InMemoryIndex

```go
index := rag.NewInMemoryIndex()
index.AddDocuments(ctx, docs)
results, _ := index.Query(ctx, "search query", 10)
```

### QdrantIndex

```go
// Requires a *qdrant.Client from github.com/qdrant/go-client/qdrant
qdrantClient, _ := qdrant.NewClient(&qdrant.Config{Host: "localhost", Port: 6334})
index, _ := rag.NewQdrantIndex(rag.QdrantConfig{
    Client:     qdrantClient,
    Collection: "my-docs",
})
```

### QdrantTextIndex

Combines embedding generation with Qdrant storage — no need to pre-compute vectors:

```go
qdrantClient, _ := qdrant.NewClient(&qdrant.Config{Host: "localhost", Port: 6334})
embedder, _ := embedding.NewOpenAIEmbeddingModel(...)
index, _ := rag.NewQdrantTextIndex(rag.QdrantTextConfig{
    Client:     qdrantClient,
    Collection: "my-docs",
    Embedder:   embedder,
})
// AddDocuments automatically embeds text before storing
index.AddDocuments(ctx, docs)
```

### Reranked Retrieval

Improve retrieval precision by wrapping an Index with a Reranker:

```go
rerankedIdx := rag.NewRerankedIndex(baseIndex, myReranker, 3)
results, _ := rerankedIdx.Query(ctx, "search query", 5)
```

## Reading images (upstream #2114)

The Read tool returns image files (`.png .jpg .jpeg .gif .webp .bmp .tiff .ico`)
as base64 `DataBlock` results instead of mojibake text, on both the host and
workspace-backend paths. PDF page rendering is not supported (it would require
a rasterizer dependency).

Every image response also carries a leading text placeholder such as
`[shot.png: image/png, 12345 bytes]`. The agent's tool pipeline is
string-based, so without it an image-only result would reach the model as an
empty string and read as "this file is empty".

Whether the model actually sees the pixels depends on the provider:

| Provider path            | What the model receives                  |
| ------------------------ | ---------------------------------------- |
| OpenAI **Responses** API | native `input_image` parts (upstream #2389) |
| Chat Completions, Anthropic, Gemini, DashScope | the text placeholder only |

There is no capability probe yet (upstream checks `model_input_types`), so
Read cannot refuse to return an image for a model that cannot see it — it
degrades to the placeholder.

Images larger than `tool.MaxInlineImageBytes` (256 KB) are reported as text
instead of inlined. Base64 costs roughly its own file size in estimated tokens,
so a single 1 MB screenshot would consume on the order of 250k tokens and
trigger an immediate compression.

`ToolResultBlock.Output` is a plain `string` for text-only results and a
`[]message.ContentBlock` when a tool returned something non-text. Use
`GetOutputText()` when you only want the text.

## Agent-driven compression (upstream #2143)

```go
a := agent.NewUnifiedAgent("bot", "...", cm,
    agent.WithToolkit(tk),
    agent.WithContextConfig(&agent.ContextConfig{
        TriggerRatio: 0.8, // automatic compression fires here
        // AgentDrivenTriggerRatio: 0.4, // default is TriggerRatio/2
    }),
    agent.WithAgentDrivenCompression(), // registers the compress_context tool
)
```

The model can call `compress_context` itself when history grows unwieldy,
instead of waiting for the automatic token-threshold compression.

Three things to know:

- **It compresses at a lower threshold than the automatic path**
  (`ContextConfig.AgentDrivenTriggerRatio`, default `TriggerRatio/2`). With the
  same threshold the agent would always compress first and the model could
  never trigger the tool.
- **Its reply is honest.** When the context is still below that threshold the
  tool says nothing was compressed. Claiming success on a no-op teaches the
  model that details are still available when they are not.
- **It runs sequentially and needs no confirmation.** The tool rewrites the
  shared message history, so it is `ConcurrencySafe: false` and never joins a
  parallel tool batch, and it returns an allow decision so an ASK-mode
  permission engine does not stall every model-initiated compression.

The compression split also keeps unfinished tool calls out of the summarized
portion: when the model calls `compress_context` from inside the acting loop,
the assistant message holding the batch's tool calls is already in the context,
and summarizing it away would orphan the results that land moments later.

For custom pipelines, wire it manually with `tool.NewCompressContextTool(fn)`,
where `fn` is a `tool.CompressFunc` returning a `tool.CompressionResult`:

```go
tk := tool.NewToolkit(
    tool.ReadTool(),
    tool.NewCompressContextTool(func(ctx context.Context) (tool.CompressionResult, error) {
        compressed, err := myCompressor.Run(ctx)
        return tool.CompressionResult{Compressed: compressed}, err
    }),
)
```

## Schema-guided argument repair (upstream #2496)

Tool-call arguments are repaired in two layers: malformed JSON goes through
syntax repair, and **every** call is coerced toward the tool's input schema —
quoted numbers (`"limit":"5"` → `5`), stringified booleans, lone values
wrapped into arrays, stringified objects parsed. Coercion never drops keys;
uncoercible values reach JSON-Schema validation unchanged and fail loudly.

## Read cache in workspaces (upstream #2092)

The read cache validates freshness against the filesystem that served the
read: backends implementing the optional `tool.BackendStatter` interface
(e.g. `workspace.ToolBackend` via POSIX stat) provide their own mtime, and
backend writes/edits invalidate cached copies — so `Read` → `Edit` works
inside Docker/K8s/E2B workspaces.

## WASM Sandbox

The CLI implementation enforces fuel, linear-memory, timeout, directory-grant, and output-capture settings through Wasmtime. Wasmer and wasm3 may be discovered, but execution with the default resource limits returns `ErrUnsupportedLimits`. Select Wasmtime explicitly and check both errors and result status. Empty `AllowedPaths` grants no host directories.

```go
rt, err := wasm.NewCLIRuntime("wasmtime")
if err != nil { log.Fatal(err) }
sandbox := wasm.NewSandbox(wasm.SandboxConfig{
    Runtime:        rt,
    MaxMemory:      64 * 1024 * 1024,
    MaxDuration:    5 * time.Second,
    MaxOutputBytes: 1024 * 1024,
})
result, err := sandbox.Run(ctx, "tools/transform.wasm", []byte(`{"text":"hello"}`))
if err != nil { log.Fatal(err) }
if result.ExitCode != 0 || result.OutputTruncated {
    log.Fatalf("WASM exit=%d, output truncated=%v", result.ExitCode, result.OutputTruncated)
}
fmt.Println(string(result.Stdout))
```

This is complementary to workspace sandboxing (Docker, K8s, etc.) — WASM provides lighter-weight isolation without requiring container infrastructure. See [Deployment](deployment.md) for more sandbox options.

## Permission System

Every tool execution goes through the permission engine:

| Mode | Behavior |
|------|----------|
| `Default` | Requires explicit allow rules or user confirmation |
| `AcceptEdits` | Allows file modifications, asks for shell commands |
| `Explore` | Read-only operations only |
| `Bypass` | Allows everything (for sandboxed environments) |
| `DontAsk` | Denies anything that would normally ask |

```go
permCtx := permission.NewContext(permission.ModeAcceptEdits)
a := agent.NewUnifiedAgent("bot", "...", cm,
    agent.WithPermissionContext(permCtx),
)
```

## Tool-level Middleware

Attach middleware to individual tools:

```go
type AuditMiddleware struct{}

func (m *AuditMiddleware) Wrap(ctx context.Context, name string, input map[string]any, next tool.ToolHandler) (any, error) {
    log.Printf("tool %s called with %v", name, input)
    return next(ctx, name, input)
}

myTool.AddMiddleware(&AuditMiddleware{})
```

## See Also

- [Architecture](architecture.md) — Tool system in the broader design
- [Middleware](middleware.md) — Agent-level middleware (including OnActing hook)
- [Deployment](deployment.md) — Workspace sandboxing for tool execution
- [Go Runtime Features](go-exclusive.md) — WASM sandbox details
