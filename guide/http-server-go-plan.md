# Build Your Own HTTP Server From Scratch in Go

A project-based learning plan adapted from *Build Your Own Web Server From Scratch in Node.JS* by James Smith, restructured for Go and broken down using an education-first approach.

---

## Why Go Instead of Node.js?

Go is arguably a *better* language for this project than Node.js. The book's author even references Go's standard library as a model of good design (e.g., `io.Reader`, `io.Writer`, `bufio.Writer`). In Go, you get goroutines instead of event-loop callbacks, explicit buffer management via slices, and interfaces that map beautifully to the abstractions this project teaches. You'll fight fewer framework abstractions and get closer to the metal.

---

## Part 1: TCP Foundation (Week 1)

**Time:** 6–8 hours  
**Goal:** Get comfortable with Go's `net` package by building a TCP echo server, then evolve it into a message-based protocol server.

---

### Subproblem 1.1: TCP Echo Server

**What you're building:** A server that listens on a port, accepts connections, reads bytes from each client, and writes those same bytes back. One goroutine per connection.

#### Logic Walkthrough

The core loop of any TCP server in any language follows the same four-step dance: bind, listen, accept, handle. Here's how it works in Go.

You call `net.Listen("tcp", ":1234")` which gives you back a `net.Listener`. This is Go's abstraction over the listening socket — the OS is now reserving port 1234 for you. Then you sit in an infinite loop calling `listener.Accept()`. Each call blocks until a client connects, then hands you back a `net.Conn` — Go's abstraction over the connection socket. You spin off `go handleConn(conn)` so you can accept the next client without waiting.

Inside `handleConn`, you're doing the simplest possible thing: read into a byte slice with `conn.Read(buf)`, then write those same bytes back with `conn.Write(buf[:n])`. `conn.Read` returns the number of bytes actually read (which may be fewer than the buffer size — this is important later) and an error. When the client disconnects, you get `io.EOF` as the error, and that's your signal to break out of the loop and close the connection.

Key decisions you'll make:

- **Buffer size:** Start with `make([]byte, 4096)`. The size affects how many bytes you read per syscall. Too small = too many syscalls. Too large = wasted memory per connection. 4KB is fine for learning.
- **When to close:** Use `defer conn.Close()` at the top of your handler. This is Go's equivalent of try-finally — it guarantees cleanup even if you panic.
- **Error handling:** `conn.Read` returns `(0, io.EOF)` on normal disconnect. Any other error means something went wrong. Log it and bail.

**Gotcha — TCP is a byte stream, not messages:** If a client sends "hello" and then "world" very quickly, you might receive "helloworld" in a single `Read`. Or you might receive "hel" and then "loworld". TCP does not preserve write boundaries. This is the #1 beginner trap in network programming, and the entire next subproblem exists because of it.

**Testing:** Use `nc` (netcat) or `socat` to connect: `nc localhost 1234`. Type something, see it echo back. Send "q" and have your server close the connection.

#### Reading Resource
Go documentation: [net package — TCP connections](https://pkg.go.dev/net) — read the `Listener`, `Conn`, and `TCPConn` type docs carefully.

#### YouTube Search
`"golang tcp server tutorial from scratch"`

#### Where You'll See This Again
1. **Redis server:** Redis accepts TCP connections and reads/writes a custom protocol (RESP) over them — same accept-loop-handle pattern you're building here.
2. **Game servers:** Multiplayer game backends use TCP (or UDP) servers with one goroutine per player connection, reading game actions and writing state updates.
3. **Database drivers:** When your Go app connects to PostgreSQL, the database driver is doing exactly this in reverse — it's the *client* side of a TCP connection, reading and writing the Postgres wire protocol.

This subproblem matters because every networked application you'll ever build sits on top of this accept-read-write loop. Understanding it at the socket level means you'll debug connection issues, timeouts, and performance problems from a place of understanding rather than guesswork.

---

### Subproblem 1.2: Goroutines and Concurrency Model

**What you're building:** Nothing new code-wise — but you need to deeply understand *why* `go handleConn(conn)` works and how it differs from the Node.js event loop.

#### Logic Walkthrough

In Node.js, the entire server runs on a single OS thread inside an event loop. You register callbacks, and the runtime calls them when IO is ready. You must *never* block, or everything freezes. That's why the book spends two full chapters on converting callbacks to promises.

Go takes a completely different approach. When you write `go handleConn(conn)`, Go spawns a *goroutine* — a lightweight, user-space thread managed by Go's runtime scheduler. Each goroutine can call blocking operations like `conn.Read()` and the Go runtime will transparently park it and schedule another goroutine onto the OS thread. You get the *programming model* of blocking IO (simple, sequential code) with the *performance* of non-blocking IO.

This means:

- You don't need promises, async/await, or callbacks. Your code reads top-to-bottom.
- You don't need to convert between event-based and sequential styles (which the book spends all of Chapter 4 on). In Go, you just... write sequential code.
- Backpressure is automatic. If `conn.Write` can't send because the TCP send buffer is full, the goroutine blocks. No unbounded internal queues like in Node.js. The book's entire Section 4.5 on backpressure? In Go, you get it for free with blocking IO.
- You *do* need to think about shared state. If two goroutines access the same map or counter, you need a mutex or channel. Node.js avoids this by being single-threaded, but Go gives you the tools to handle it.

**Key insight:** The book's TCPConn wrapper with promise-based `soRead`/`soWrite` was necessary because Node.js's event model doesn't naturally support sequential IO. In Go, `net.Conn` already *is* the promise-based wrapper — `conn.Read()` blocks and returns data, like `await soRead()` but without the ceremony.

#### Reading Resource
[Effective Go — Goroutines](https://go.dev/doc/effective_go#goroutines) — the official guide on goroutines and channels.

#### YouTube Search
`"goroutines vs event loop concurrency model explained"`

#### Where You'll See This Again
1. **Web frameworks (Gin, Echo, Chi):** Every HTTP request handler runs in its own goroutine. When you write middleware or handlers, you're writing sequential code that the runtime multiplexes across OS threads.
2. **gRPC servers:** gRPC-Go spawns a goroutine per stream, and your handler can do blocking IO (database queries, downstream calls) without worrying about event-loop starvation.
3. **Cloud infrastructure (Kubernetes, Docker):** These are written in Go specifically because the goroutine model makes it natural to manage thousands of concurrent network connections.

This matters because Go's concurrency model eliminates an entire *category* of complexity that the book's first four chapters are dedicated to solving. Understanding why lets you skip that complexity and focus on the protocol itself.

---

### Subproblem 1.3: A Newline-Delimited Message Protocol

**What you're building:** A server where clients send newline-terminated messages. If the message is "quit", reply "Bye.\n" and close. Otherwise, reply "Echo: {msg}\n".

#### Logic Walkthrough

This is where you confront the byte-stream nature of TCP head-on. A single `conn.Read()` might give you half a message, two messages, or one and a half messages. You need a *buffer* to accumulate data and a *parser* to extract complete messages.

The algorithm:

1. Maintain a buffer (a `[]byte` slice that grows as needed).
2. Each loop iteration: try to find `\n` in the buffer.
3. If found: extract everything up to and including `\n` — that's one complete message. Remove it from the buffer. Process it.
4. If not found: call `conn.Read()` to get more data, append it to the buffer, and try again.
5. If `conn.Read()` returns `io.EOF` and the buffer is non-empty, you have an incomplete message — that's a protocol error.

In Go, you can use `bytes.IndexByte(buf, '\n')` to find the delimiter. For the buffer itself, a `[]byte` slice works — Go slices are already dynamic arrays with the amortized O(1) append behavior that the book manually implements with `DynBuf`.

**But here's the cleaner Go approach:** Use `bufio.Reader`. Wrap your `net.Conn` in `bufio.NewReader(conn)`, and then call `reader.ReadString('\n')`. This does all the buffering and delimiter-scanning for you. The book implements its own `DynBuf` because Node.js doesn't have this. Go does.

Key decisions:

- **Maximum message size:** Cap it. The book uses 8KB for HTTP headers. For this toy protocol, 4KB is fine. If you hit the limit, close the connection. This prevents a malicious client from eating all your memory.
- **What about pipelining?** A client might send "hello\nworld\nquit\n" in a single TCP write. Your parser must handle this — the remaining data stays in the buffer for the next loop iteration. Test this with: `echo -e 'hello\nworld\nquit' | nc localhost 1234`

**Gotcha — bufio.Scanner vs bufio.Reader:** `bufio.Scanner` splits on lines too, but it has a max token size (default 64KB) and silently drops data beyond it. `bufio.Reader.ReadString` is better for protocol work because you get explicit error handling.

#### Reading Resource
Go documentation: [bufio package](https://pkg.go.dev/bufio) — focus on `Reader`, `ReadString`, and `ReadBytes`.

#### YouTube Search
`"TCP message framing protocol parsing golang"`

#### Where You'll See This Again
1. **Redis protocol (RESP):** Redis uses `\r\n` as delimiters in its text protocol. Parsing RESP is essentially this same "buffer data, scan for delimiter, extract message" loop.
2. **SMTP (email):** The SMTP protocol is line-based with `\r\n` terminators. Email servers parse commands exactly this way.
3. **Log processing pipelines:** Tools like `tail -f | grep` operate on newline-delimited streams. Your message parser is doing the same structured extraction from a byte stream.

This subproblem is the conceptual backbone of HTTP parsing. HTTP headers are delimited by `\r\n`, and the header-body boundary is `\r\n\r\n`. If you can parse newline-delimited messages, you're 80% of the way to parsing HTTP headers.

---

## Part 2: HTTP/1.1 Protocol Core (Weeks 2–3)

**Time:** 12–16 hours  
**Goal:** Parse HTTP requests and generate HTTP responses. Support keep-alive connections. End up with a working HTTP server that can respond to curl and browsers.

---

### Subproblem 2.1: HTTP Message Structure & Header Parsing

**What you're building:** A function that takes a buffered TCP connection and extracts a fully parsed HTTP request (method, URI, version, headers).

#### Logic Walkthrough

An HTTP request looks like this on the wire:

```
GET /index.html HTTP/1.1\r\n
Host: localhost:1234\r\n
Content-Length: 0\r\n
\r\n
```

The header ends with a blank line (`\r\n\r\n`). So your parsing strategy is:

1. **Accumulate bytes until you see `\r\n\r\n`.** This means the complete header is in your buffer. Cap the header at 8KB — no legitimate request needs more, and this prevents memory abuse.
2. **Split the header into lines** by `\r\n`.
3. **Parse the first line (request line):** It's three tokens separated by spaces — method, URI, version. For example: `GET`, `/index.html`, `HTTP/1.1`.
4. **Parse each subsequent line as a header field:** Split on the first `:`. The part before is the field name (case-insensitive!), the part after (trimmed of whitespace) is the value.
5. **Store headers** as a slice of key-value pairs. Don't use a map because HTTP allows duplicate header names (e.g., multiple `Set-Cookie`).

Define your types first:

```go
type HTTPReq struct {
    Method  string
    URI     string
    Version string
    Headers []Header
}

type Header struct {
    Name  string
    Value string
}
```

For finding `\r\n\r\n` in the buffer, the approach is the same as the newline protocol: keep reading from the connection and appending to a buffer until you find the sentinel. You can use `bytes.Index(buf, []byte("\r\n\r\n"))`.

**Gotcha — `\r\n` vs `\n`:** HTTP mandates `\r\n` (CRLF) line endings. Some clients only send `\n`. Decide your tolerance level. For learning, strict `\r\n` is fine. In production, you'd want to be lenient (Postel's law).

**Gotcha — Header field names are case-insensitive:** `Content-Length` and `content-length` are the same. Use `strings.EqualFold` when comparing, or normalize to lowercase on parse.

**Gotcha — the body comes after the header:** After you extract the header (everything before `\r\n\r\n`), whatever remains in your buffer might be the start of the request body. Don't discard it.

#### Reading Resource
[RFC 9112 — HTTP/1.1 Message Syntax](https://www.rfc-editor.org/rfc/rfc9112.html) — Sections 2 (Message) and 3 (Request Line). Read the BNF notation; it's more precise than any tutorial.

#### YouTube Search
`"HTTP 1.1 request format protocol deep dive"`

#### Where You'll See This Again
1. **API gateways (Envoy, Traefik):** These proxies parse HTTP headers at extremely high speed to make routing decisions. The same header-parsing logic, but optimized for zero-allocation paths.
2. **Web Application Firewalls:** WAFs inspect HTTP headers for malicious patterns (SQL injection in query strings, oversized headers). The parsing is identical to what you're building.
3. **HTTP client libraries (Go's `net/http`):** When your Go code calls `http.Get()`, the response is parsed by code that does exactly this: read until `\r\n\r\n`, split into lines, extract status code and headers.

This is the most important parsing code in the project. Every subsequent feature (body reading, chunked encoding, range requests, caching) depends on correctly extracting the header and its fields.

---

### Subproblem 2.2: Request Body Reading with Content-Length

**What you're building:** After parsing the header, read exactly `N` bytes of the body where `N` comes from the `Content-Length` header.

#### Logic Walkthrough

HTTP bodies are tricky because the header and body are separated only by that blank line — the body is just "the rest." But how much is "the rest"? That's what `Content-Length` tells you.

After you've parsed the header and consumed the `\r\n\r\n`, you might already have some body bytes in your buffer (from an earlier read that grabbed more than just the header). So your body reader needs to:

1. Look up `Content-Length` from the parsed headers.
2. Check how many body bytes you already have in the buffer.
3. Read the remaining bytes from the connection until you've got exactly `Content-Length` bytes total.

Model this as a Go `io.Reader` — define a struct that tracks how many bytes remain and reads from the underlying connection:

```go
type LimitedBodyReader struct {
    conn      net.Conn
    buf       *bufio.Reader
    remaining int
}
```

Its `Read(p []byte)` method: if `remaining == 0`, return `(0, io.EOF)`. Otherwise, limit the read to `min(len(p), remaining)`, read from the buffered reader, decrement remaining by bytes read, and return.

**Key design decision — io.Reader interface:** In Go, the `io.Reader` interface (`Read(p []byte) (n int, err error)`) is the universal abstraction for "something you can read bytes from." Files, network connections, gzip decompressors, and your body reader all implement it. This is the Go equivalent of the book's `BodyReader` type, but it's a standard interface that works with the entire standard library.

**Gotcha — GET and HEAD have no body:** Even if a `Content-Length` header is present on a GET request, ignore it. The spec says these methods have no body.

**Gotcha — the body might not fit in memory:** Don't read the entire body into a `[]byte`. Instead, expose it as an `io.Reader` so consumers can stream it. This is how you handle a 2GB file upload without running out of memory.

#### Reading Resource
Go documentation: [io.Reader and io.LimitReader](https://pkg.go.dev/io#LimitReader) — `io.LimitReader` already does most of what you need. Study its source code (it's ~20 lines).

#### YouTube Search
`"HTTP content-length body parsing explained"`

#### Where You'll See This Again
1. **File upload handling:** When a user uploads a file via a web form, the server reads the body using `Content-Length` to know when the upload is complete. Frameworks like Gin wrap this in convenience methods, but underneath it's the same limited reader.
2. **API request parsing:** When your REST API receives a JSON body, `json.NewDecoder(r.Body)` is reading from a body reader just like the one you're building.
3. **Proxy servers:** A forward proxy must read the request body to forward it to the upstream server. It uses `Content-Length` to know how many bytes to copy.

This is the second of three body-reading strategies (the others are chunked encoding and read-to-EOF). Getting it right means your server can handle POST/PUT requests, which is essential for any non-trivial HTTP application.

---

### Subproblem 2.3: Generating HTTP Responses

**What you're building:** A function that takes a status code, headers, and a body source, and writes a properly formatted HTTP response to the connection.

#### Logic Walkthrough

The response format mirrors the request:

```
HTTP/1.1 200 OK\r\n
Content-Length: 13\r\n
Server: my-go-server\r\n
\r\n
hello world.\n
```

Your response writer needs to:

1. **Write the status line:** `HTTP/1.1 {code} {reason}\r\n`. Map common codes to reason phrases (200 → "OK", 404 → "Not Found", etc.).
2. **Calculate and add `Content-Length`:** If the body length is known (it's an in-memory buffer or a file with a known size), set `Content-Length`. If unknown, you'll use chunked encoding later.
3. **Write each header field:** `{Name}: {Value}\r\n` for each header.
4. **Write the blank line:** `\r\n`.
5. **Write the body:** Copy bytes from the body source to the connection.

**Critical optimization — buffer the header.** Don't do separate `conn.Write()` calls for each header line. That generates tiny TCP packets and kills performance (this is the Nagle's algorithm problem the book discusses). Instead, build the entire header in a `bytes.Buffer`, then write it in one shot:

```go
var headerBuf bytes.Buffer
fmt.Fprintf(&headerBuf, "HTTP/1.1 %d %s\r\n", code, reason)
for _, h := range headers {
    fmt.Fprintf(&headerBuf, "%s: %s\r\n", h.Name, h.Value)
}
headerBuf.WriteString("\r\n")
conn.Write(headerBuf.Bytes())
```

Or even better, use `bufio.Writer` wrapping the connection. Write all the header lines to it, then call `Flush()`. The book explicitly recommends this Go pattern in Section 7.4.

For the body, use `io.Copy(conn, bodyReader)` to stream from the body source to the connection. This handles arbitrarily large bodies without loading them into memory.

**Gotcha — disable Nagle's algorithm:** Call `conn.(*net.TCPConn).SetNoDelay(true)` to prevent the OS from buffering small writes. Combined with your application-level buffering, this gives you the best of both worlds: few syscalls *and* low latency.

#### Reading Resource
Go documentation: [bufio.Writer](https://pkg.go.dev/bufio#Writer) — study how `Write` buffers and `Flush` sends. This is the exact pattern the book recommends.

#### YouTube Search
`"HTTP response format structure explained beginner"`

#### Where You'll See This Again
1. **Reverse proxies (nginx, Caddy):** When a reverse proxy forwards a response, it reads the upstream's response header, potentially modifies some fields (adds `X-Forwarded-For`, adjusts `Content-Length`), and writes a new response to the client. Same format, same writer.
2. **Server-Sent Events (SSE):** SSE responses are HTTP responses with `Transfer-Encoding: chunked` where each chunk is an event. The response writer you build here is the foundation.
3. **Go's `net/http` ResponseWriter:** When you write `w.WriteHeader(200)` and `w.Write(data)` in an `http.Handler`, Go's stdlib is doing exactly what you're building: formatting the status line, serializing headers, and flushing the body.

This completes the request-response cycle. After this subproblem, you have a functioning HTTP server that curl and browsers can talk to.

---

### Subproblem 2.4: The Server Loop — Keep-Alive and Pipelining

**What you're building:** A loop per connection that handles multiple sequential HTTP requests, supporting HTTP/1.1 keep-alive.

#### Logic Walkthrough

HTTP/1.0 closes the connection after every request-response pair. HTTP/1.1 keeps it open by default, allowing multiple requests on the same connection. Your handler goroutine needs to loop:

1. Parse one HTTP request header.
2. Build a body reader for it.
3. Route the request to a handler function, get a response.
4. Write the response.
5. **Drain any unread request body.** The handler might have ignored the body (e.g., for a GET request that incorrectly includes one, or a POST handler that doesn't care about the body). You *must* consume the remaining body bytes so the parser is positioned correctly for the next request header.
6. If the request was HTTP/1.0, close the connection. Otherwise, loop back to step 1.

The drain step is subtle but critical. If the client sent a POST with a 1MB body and your handler didn't read it, those bytes are still sitting in the TCP buffer. The next `ReadString('\r\n')` call would try to parse body bytes as an HTTP header — garbage. So after writing the response, you do `io.Copy(io.Discard, bodyReader)` to throw away any unread body.

**Pipelining:** A client can send multiple requests without waiting for responses. This means after parsing one request header, there might already be *another* request header (or the start of one) in your buffer. Your parser handles this naturally — the buffer retains leftover data between iterations.

**Gotcha — error recovery:** If you get a malformed request, you usually can't recover. Send a 400 Bad Request response and close the connection, because you don't know where the bad request ends and the next one begins.

#### Reading Resource
[RFC 9112 Section 9 — Connection Management](https://www.rfc-editor.org/rfc/rfc9112.html#section-9) — covers keep-alive, pipelining, and connection closure.

#### YouTube Search
`"HTTP keep-alive connection reuse explained"`

#### Where You'll See This Again
1. **Connection pooling in HTTP clients:** Go's `http.Client` reuses connections by default. The other end of your keep-alive loop is the client's pool: it sends a request, reads the response, and sends another request on the same `net.Conn`.
2. **Database connection pools:** PostgreSQL's wire protocol is also request-response over a persistent TCP connection. Connection pools reuse connections the same way.
3. **Load balancers:** When a load balancer like HAProxy maintains persistent connections to backend servers, it's managing exactly this kind of keep-alive loop on both sides.

This turns your server from a toy that handles one request per connection into something production-adjacent. Keep-alive is essential for real-world performance — it eliminates the TCP handshake overhead for every request.

---

## Part 3: Dynamic Content & Chunked Encoding (Week 3)

**Time:** 6–8 hours  
**Goal:** Support responses where the content length isn't known in advance. This enables streaming, dynamic page generation, and later, compression.

---

### Subproblem 3.1: Chunked Transfer Encoding (Response)

**What you're building:** When your response body's length is unknown, wrap it in chunked transfer encoding so the client knows when the body ends.

#### Logic Walkthrough

Chunked encoding is simple: instead of one big blob prefixed by `Content-Length`, you send a series of "chunks." Each chunk is: `{hex-length}\r\n{data}\r\n`. A zero-length chunk signals the end.

Example on the wire:
```
4\r\n
HTTP\r\n
6\r\n
server\r\n
0\r\n
\r\n
```

Your response writer needs a new code path:

1. If the body's length is known → set `Content-Length`, write body directly.
2. If the body's length is unknown → set `Transfer-Encoding: chunked`, then for each chunk of data from the body reader, write `{hex(len)}\r\n{data}\r\n`. When the body reader returns `io.EOF`, write `0\r\n\r\n`.

In Go, you can implement this as a `ChunkedWriter` that wraps a `net.Conn` and implements `io.Writer`:

```go
type ChunkedWriter struct {
    w io.Writer
}

func (cw *ChunkedWriter) Write(p []byte) (int, error) {
    if len(p) == 0 {
        return 0, nil
    }
    // Write: "{hex-length}\r\n{data}\r\n"
    _, err := fmt.Fprintf(cw.w, "%x\r\n", len(p))
    if err != nil { return 0, err }
    n, err := cw.w.Write(p)
    if err != nil { return n, err }
    _, err = cw.w.Write([]byte("\r\n"))
    return n, err
}

func (cw *ChunkedWriter) Close() error {
    _, err := cw.w.Write([]byte("0\r\n\r\n"))
    return err
}
```

Then your response writer can do: `io.Copy(chunkedWriter, bodyReader)` and call `chunkedWriter.Close()`.

**Key Go insight:** In the book, the author uses JavaScript generators (async generator functions with `yield`) to produce chunked data. In Go, you use goroutines and channels, or more idiomatically, an `io.Pipe()`. A goroutine writes to the pipe writer, and the response loop reads from the pipe reader. The pipe provides automatic backpressure — the writer blocks when the reader isn't consuming.

#### Reading Resource
[RFC 9112 Section 7 — Transfer Codings](https://www.rfc-editor.org/rfc/rfc9112.html#section-7) — the authoritative spec for chunked encoding format.

#### YouTube Search
`"HTTP chunked transfer encoding how it works"`

#### Where You'll See This Again
1. **Server-Sent Events:** SSE uses chunked encoding to push events from server to client indefinitely. Each event is a chunk.
2. **Streaming JSON APIs:** APIs that return large result sets sometimes stream JSON objects one per chunk, so the client can start processing before the full result is ready.
3. **`docker logs --follow`:** When Docker streams container logs to your terminal, it's using chunked HTTP responses behind the scenes.

Chunked encoding unlocks dynamic content and streaming. Without it, your server must know the response size before sending the first byte, which means buffering the entire response in memory.

---

### Subproblem 3.2: Chunked Transfer Encoding (Request)

**What you're building:** Parse incoming request bodies that use chunked encoding instead of `Content-Length`.

#### Logic Walkthrough

This is the reverse of 3.1. The client sends chunks, and you decode them into a byte stream. Your body reader needs to:

1. Read the chunk-size line (read until `\r\n`, parse the hex number).
2. Read exactly that many bytes of chunk data.
3. Consume the trailing `\r\n`.
4. If the chunk size was 0, you've hit the end — return `io.EOF`.
5. Repeat.

Implement this as a struct that satisfies `io.Reader`. Track state: are you in the middle of reading a chunk, or between chunks? When `Read(p)` is called:

- If you have remaining bytes in the current chunk, read up to `min(len(p), remaining)` from the underlying connection. Decrement remaining.
- If remaining is 0, you've finished a chunk. Read the trailing `\r\n`, then read the next chunk-size line. If it's 0, return EOF.

**Gotcha — don't wait for the full chunk before returning data.** If a chunk is 1MB, don't buffer it. Return data as it arrives from the connection. This is what the book emphasizes: chunks are protocol framing, not application messages.

**Testing:** Use `curl -T- http://localhost:1234/echo` which streams stdin as a chunked request. Or construct raw chunked requests with `socat`.

#### Reading Resource
[Go's `net/http/internal/chunked` source](https://cs.opensource.google/go/go/+/refs/tags/go1.22.0:src/net/http/internal/chunked.go) — read how Go's stdlib implements chunked decoding. It's clean, well-commented, and exactly what you're building.

#### YouTube Search
`"implement chunked transfer encoding parser from scratch"`

#### Where You'll See This Again
1. **WebSocket upgrade:** After the HTTP upgrade handshake, the rest of the connection is WebSocket frames. The concept of "parse a frame header, read N bytes of payload, repeat" is identical.
2. **HTTP/2 frame parsing:** HTTP/2 uses fixed-format binary frames with explicit length fields. Same pattern: read header, read payload, repeat.
3. **Protocol Buffers over streams:** When sending protobuf messages over a stream, each message is length-prefixed. The decode loop is the same structure.

Handling chunked requests completes your server's ability to handle any HTTP/1.1 body encoding. This is required before you can build a robust echo server or handle streaming uploads.

---

## Part 4: Static File Server (Week 4)

**Time:** 8–10 hours  
**Goal:** Serve files from disk with proper resource management, range requests for resumable downloads, and cache validation.

---

### Subproblem 4.1: Serving Files from Disk

**What you're building:** A handler that maps URI paths to filesystem paths and streams file contents as the response body.

#### Logic Walkthrough

The sequence: open the file, stat it for size, set `Content-Length`, stream the contents, close the file.

In Go:

1. Map the URI to a file path. Strip a prefix (e.g., `/files/foo.txt` → `./static/foo.txt`). **Critically, sanitize the path** — a request for `/files/../../etc/passwd` must not escape your served directory. Use `filepath.Clean` and verify the result is still under your root.
2. Open with `os.Open(path)` (read-only). If it fails, return 404.
3. Call `file.Stat()` to get size and check it's a regular file (not a directory or symlink).
4. Set `Content-Length` to the file size.
5. Use `io.Copy(conn, file)` to stream the file to the connection. This uses `sendfile` under the hood on Linux — a zero-copy optimization where the kernel copies directly from disk to the network socket without going through userspace.
6. Close the file with `defer file.Close()`.

**Resource management — the ownership chain:** The book dedicates an entire discussion section (9.3) to this because it's tricky in Node.js where you pass ownership of the file handle to a BodyReader and must ensure cleanup. In Go, it's much simpler. Your file handle lives in the handler goroutine. You `defer file.Close()` at the top. The `io.Copy` streams everything, and when the handler returns, the defer triggers. No ownership transfer needed.

**But what if you return an io.Reader wrapping the file?** If your architecture separates "decide what to respond" from "write the response," you *do* need to handle this. Pass the `*os.File` as the body reader, and ensure the response writer closes it after streaming. In Go, `io.ReadCloser` is the interface for "a reader that must be closed."

**Gotcha — file size can change:** Between `Stat()` and finishing the copy, the file could be modified. If the file grew, you send `Content-Length` bytes and stop — fine. If it shrank, `io.Copy` will hit EOF early. The client will see fewer bytes than `Content-Length` promised and report an error. The book suggests closing the connection in this case, which is the correct approach.

#### Reading Resource
Go documentation: [os.File and filepath package](https://pkg.go.dev/os#File) — focus on `Open`, `Stat`, and `filepath.Clean` for path sanitization.

#### YouTube Search
`"golang file server http serve static files from scratch"`

#### Where You'll See This Again
1. **CDN origin servers:** When a CDN cache misses, the origin server serves the file from disk using exactly this pattern. High-performance origins use sendfile or io_uring.
2. **Object storage (MinIO):** MinIO stores objects as files and serves them over HTTP. The file-to-HTTP-response pipeline is this subproblem at scale.
3. **Container image registries:** When Docker pulls a layer, the registry serves a blob from disk as an HTTP response with `Content-Length`.

Serving static files is the original use case for HTTP. It exercises file IO, resource management, and the `io.Reader`/`io.Writer` composition that is central to Go.

---

### Subproblem 4.2: Range Requests

**What you're building:** Support the `Range` header so clients can request a portion of a file (enables resumable downloads and video seeking).

#### Logic Walkthrough

The client sends `Range: bytes=100-199` to request bytes 100 through 199 (inclusive). Your server needs to:

1. Parse the `Range` header. The format is `bytes=start-end` where end is optional (meaning "to the end") or it's `bytes=-N` meaning "the last N bytes." There can be multiple ranges separated by commas, but you can start by supporting only single ranges.
2. Compute the effective range by intersecting with the actual file size. A request for `bytes=100-9999` on a 500-byte file gives effective range [100, 500).
3. If the range is unsatisfiable (e.g., starts past the end of the file), return `416 Range Not Satisfiable` with `Content-Range: bytes */500`.
4. If the range is invalid (malformed), ignore it and serve the full file.
5. Otherwise, respond with `206 Partial Content`, set `Content-Range: bytes 100-199/500`, set `Content-Length` to the range size (100), and serve only those bytes.

To read a portion of a file in Go, use `file.Seek(offset, io.SeekStart)` to position the file pointer, then use `io.LimitReader(file, rangeLength)` as the body reader. Or use `file.ReadAt` which reads at an offset without seeking.

Also add `Accept-Ranges: bytes` to your normal (non-range) responses so clients know this feature is available.

**Supporting the HEAD method:** HEAD is identical to GET but without the response body. Implement it by running your full handler logic (including range computation and `Content-Length` calculation) but skipping the body write. This lets clients probe for range support without downloading anything.

#### Reading Resource
[RFC 9110 Section 14 — Range Requests](https://www.rfc-editor.org/rfc/rfc9110.html#section-14) — the complete specification for range handling.

#### YouTube Search
`"HTTP range requests partial content 206 explained"`

#### Where You'll See This Again
1. **Video streaming:** When you seek in a YouTube or Netflix video, the player issues a range request for the byte offset corresponding to the timestamp. The server responds with just that segment.
2. **Download managers:** Tools like wget's `--continue` flag use range requests to resume interrupted downloads from where they left off.
3. **PDF viewers in browsers:** When you open a large PDF, the browser may request only the pages you're viewing, using range requests to avoid downloading the entire file.

Range requests turn your file server into something production-usable. Without them, a failed download must restart from byte 0, and video streaming is impossible.

---

### Subproblem 4.3: HTTP Caching (Conditional Requests)

**What you're building:** Support `Last-Modified` / `If-Modified-Since` headers so browsers can validate their cache without re-downloading files.

#### Logic Walkthrough

The caching flow:

1. **First request:** Your server responds with the file and includes `Last-Modified: {file_mtime}` in the response header.
2. **Subsequent request:** The browser sends `If-Modified-Since: {cached_timestamp}`. Your server compares this to the file's current modification time.
3. **If unchanged:** Respond with `304 Not Modified` and no body. The browser uses its cache.
4. **If changed:** Respond normally with the updated file and new `Last-Modified`.

Implementation: use `file.Stat()` to get `ModTime()`. Format it as an HTTP date with `time.Time.UTC().Format(http.TimeFormat)`. Parse the incoming `If-Modified-Since` with `time.Parse(http.TimeFormat, value)`.

Compare timestamps at second resolution (HTTP dates only have second precision). If the file's mtime is ≤ the If-Modified-Since value, return 304.

For range requests, also support `If-Range`: if the file has changed since the client's cached version, ignore the `Range` header and serve the full file (because the client's partial cache is stale).

**Gotcha — timestamps have only 1-second resolution:** If a file is modified twice within the same second, the timestamp won't change. For critical applications, implement `ETag` (a hash or version number) instead. But `Last-Modified` is sufficient for a file server.

#### Reading Resource
[MDN Web Docs — HTTP Caching](https://developer.mozilla.org/en-US/docs/Web/HTTP/Caching) — a practical guide to HTTP caching with clear diagrams.

#### YouTube Search
`"HTTP caching last-modified etag conditional requests"`

#### Where You'll See This Again
1. **Browser DevTools → Network tab:** Every time you see a grayed-out "304" status for a cached resource, this is the `If-Modified-Since`/`304` flow you built.
2. **Package managers (npm, pip, apt):** When updating, they use conditional requests to check if packages have changed, avoiding unnecessary downloads.
3. **API polling:** Mobile apps that poll a REST API for updates use ETags or `If-Modified-Since` to avoid re-transferring unchanged data.

Caching is one of the most impactful HTTP features for real-world performance. A 304 response is tiny compared to re-transferring a 500KB JavaScript bundle.

---

## Part 5: Compression & Streaming Abstraction (Week 5)

**Time:** 6–8 hours  
**Goal:** Add gzip compression to responses. Learn Go's `io.Reader`/`io.Writer` composition pattern — the Go equivalent of the book's Stream/pipe discussion.

---

### Subproblem 5.1: HTTP Compression with io.Pipe

**What you're building:** If the client sends `Accept-Encoding: gzip`, compress the response body with gzip and use chunked encoding (since the compressed size is unknown).

#### Logic Walkthrough

1. Check `Accept-Encoding` for "gzip."
2. If present and the response is compressible: set `Content-Encoding: gzip`, remove `Content-Length` (since compressed size is unknown), and add `Transfer-Encoding: chunked`.
3. Wrap the response body in a gzip compressor.
4. Pipe the compressed output through your chunked writer to the connection.

Go makes this beautiful with `io.Pipe()`. Here's the pattern:

```go
pr, pw := io.Pipe()
go func() {
    gzw := gzip.NewWriter(pw)
    io.Copy(gzw, originalBody) // blocks until originalBody is exhausted
    gzw.Close()                // flush final gzip bytes
    pw.Close()                 // signal EOF to the reader
}()
// pr is now an io.Reader of gzipped data
io.Copy(chunkedWriter, pr) // stream compressed data to the client
```

The goroutine writes original body data through the gzip compressor into the pipe. The main goroutine reads compressed data from the pipe and writes chunks to the connection. The pipe provides backpressure automatically — if the network is slow, the pipe fills up, the gzip goroutine blocks on `pw.Write`, and the body reader blocks on its `Read`. Everything throttles naturally.

This is the Go equivalent of the book's `pipeline(input, gzip, socket)` from Chapter 12, but using goroutines and pipes instead of Node.js streams.

**When NOT to compress:** Already-compressed formats (JPEG, PNG, ZIP, MP4), very small responses (the gzip header overhead outweighs savings), and range responses (compression changes byte offsets, breaking ranges). Also add `Vary: Accept-Encoding` so caching proxies key on the encoding.

**Gotcha — gzip.Writer buffers internally:** For streaming responses (like the counting sheep example in the book), you need to call `gzw.Flush()` after each meaningful chunk to force output. Otherwise the client sees nothing until the gzip buffer fills. This is the exact same issue the book discusses in Section 12.4 Step 7.

#### Reading Resource
Go documentation: [compress/gzip](https://pkg.go.dev/compress/gzip) and [io.Pipe](https://pkg.go.dev/io#Pipe) — the pipe is the key composition mechanism.

#### YouTube Search
`"golang io pipe gzip compression streaming example"`

#### Where You'll See This Again
1. **Go's `net/http` server:** When you use `gzip` middleware in any Go web framework, it's doing exactly this: wrapping the `ResponseWriter` in a `gzip.Writer` and setting the headers.
2. **CI/CD artifact storage:** Build systems compress artifacts (tarballs, Docker layers) on the fly and stream them to storage. The pipe+gzip+chunked pattern is everywhere.
3. **Log aggregation:** Tools like Fluentd compress log streams before shipping them over HTTP to storage backends.

Compression is where Go's `io.Reader`/`io.Writer` composition truly shines. The book needs streams, pipes, and careful deadlock avoidance. In Go, you get composable interfaces and goroutines that make the same patterns trivial.

---

## Part 6: WebSocket & Concurrency (Weeks 5–6)

**Time:** 10–14 hours  
**Goal:** Implement WebSocket support — upgrade from HTTP, parse the binary frame format, handle bidirectional messaging. Use channels for safe concurrent communication.

---

### Subproblem 6.1: The WebSocket Handshake

**What you're building:** Detect WebSocket upgrade requests and respond with the correct handshake to establish a WebSocket connection.

#### Logic Walkthrough

A WebSocket starts as a normal HTTP request with special headers:

```
GET /chat HTTP/1.1
Upgrade: websocket
Connection: Upgrade
Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==
```

Your server needs to:

1. In the request handler, check for `Upgrade: websocket`.
2. Read the `Sec-WebSocket-Key` header.
3. Compute the accept hash: SHA-1 of `key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"`, then base64-encode.
4. Respond with `101 Switching Protocols`, the `Upgrade`, `Connection`, and `Sec-WebSocket-Accept` headers.
5. After writing the response header (no body!), the TCP connection is now a WebSocket. No more HTTP on this connection.

In Go:

```go
import "crypto/sha1"
import "encoding/base64"

func wsAcceptKey(key string) string {
    h := sha1.New()
    h.Write([]byte(key))
    h.Write([]byte("258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
    return base64.StdEncoding.EncodeToString(h.Sum(nil))
}
```

After the handshake, your server loop should *not* try to parse more HTTP requests on this connection. Pass the raw `net.Conn` (or `bufio.ReadWriter`) to the WebSocket handler.

#### Reading Resource
[RFC 6455 Section 4 — Opening Handshake](https://datatracker.ietf.org/doc/html/rfc6455#section-4) — the authoritative handshake specification.

#### YouTube Search
`"websocket handshake protocol upgrade HTTP explained"`

#### Where You'll See This Again
1. **Real-time apps (Slack, Discord):** Every chat app upgrades from HTTP to WebSocket on page load to enable real-time message delivery.
2. **Kubernetes `kubectl exec`:** When you exec into a pod, kubectl upgrades the API server connection to a WebSocket (SPDY) for the interactive terminal.
3. **GraphQL Subscriptions:** GraphQL real-time subscriptions typically run over WebSocket, starting with the same HTTP upgrade handshake.

The handshake is the bridge between HTTP and WebSocket. Getting it right means browsers can connect to your server with the standard `new WebSocket()` API.

---

### Subproblem 6.2: WebSocket Frame Parsing & Writing

**What you're building:** Read and write the binary WebSocket frame format — the payload carrier for all WebSocket messages.

#### Logic Walkthrough

WebSocket frames are *binary*, not text. This is a refreshing change from HTTP's text format. The frame structure:

- **Byte 0:** FIN flag (1 bit) + opcode (4 bits). FIN=1 means this is the last (or only) frame of a message.
- **Byte 1:** MASK flag (1 bit) + payload length (7 bits).
- **Extended length:** If the 7-bit length is 126, the next 2 bytes are the real length (big-endian uint16). If 127, the next 8 bytes (big-endian uint64).
- **Masking key:** If MASK=1, the next 4 bytes are an XOR mask. Client-to-server frames *must* be masked; server-to-client *must not*.
- **Payload:** The actual data, XOR'd with the mask if present.

To read a frame:

1. Read 2 bytes to get FIN, opcode, MASK, and initial length.
2. Read extended length bytes if needed.
3. Read 4-byte mask if MASK is set.
4. Read `length` bytes of payload.
5. If masked, XOR each byte with `mask[i % 4]`.

To write a frame: reverse the process. Server-to-client frames are *not* masked, which simplifies writing.

Use Go's `encoding/binary` package for big-endian integer handling: `binary.BigEndian.Uint16(buf)` and `binary.BigEndian.PutUint16(buf, val)`.

**Opcodes you need:** 0x01 (text), 0x02 (binary), 0x08 (close), 0x09 (ping), 0x0A (pong). When you receive a ping, respond with a pong containing the same payload.

**Fragmentation:** A large message can span multiple frames. The first frame has opcode 0x01 or 0x02 with FIN=0. Continuation frames have opcode 0x00. The last frame has FIN=1. You can start simple by only supporting unfragmented messages (FIN=1 on the first frame).

#### Reading Resource
[RFC 6455 Section 5 — Data Framing](https://datatracker.ietf.org/doc/html/rfc6455#section-5) — the binary format specification with diagrams.

#### YouTube Search
`"websocket frame format binary protocol parsing tutorial"`

#### Where You'll See This Again
1. **HTTP/2 framing:** HTTP/2 also uses binary frames with type, length, and flags fields. If you can parse WebSocket frames, HTTP/2 frames are structurally similar.
2. **MQTT protocol:** The IoT messaging protocol MQTT uses a similar variable-length encoding for packet sizes, with the same "small lengths are 1 byte, larger lengths use more bytes" pattern.
3. **Protocol Buffers wire format:** Protobuf's varint encoding for field lengths is conceptually similar — compact representation for small values, extensible for large ones.

Binary protocol parsing is a fundamental systems skill. WebSocket frames are a great introduction because the format is simple, well-documented, and you can test with any web browser.

---

### Subproblem 6.3: Concurrent WebSocket Messaging with Channels

**What you're building:** A safe bidirectional WebSocket handler where multiple goroutines can send messages concurrently without corrupting the TCP stream.

#### Logic Walkthrough

The core problem: WebSocket is full-duplex. Your app might have one goroutine reading user input and another sending periodic updates. If two goroutines write frames to the same `net.Conn` simultaneously, the frames interleave and corrupt the stream.

The book solves this with a blocking queue (Chapter 13.4) — essentially a Go channel reimplemented in JavaScript. In Go, you just use a channel.

The architecture:

1. **Read goroutine:** Loops reading frames from the connection, assembles them into messages, and sends them on a `recvChan`.
2. **Write goroutine:** Loops receiving messages from a `sendChan` and writes them as frames to the connection. This is the *only* goroutine that writes to the connection.
3. **Application goroutines:** Read from `recvChan` and write to `sendChan`. Multiple goroutines can safely write to `sendChan` because Go channels are goroutine-safe.

```go
type WSConn struct {
    RecvChan <-chan WSMessage    // app reads from this
    SendChan chan<- WSMessage    // app writes to this
    Done     <-chan struct{}     // closed when connection ends
}
```

**Graceful shutdown:** When the connection closes (the read goroutine gets an error or a close frame), close `RecvChan` so the app knows. When the app wants to close, close `SendChan` so the write goroutine sends a close frame and exits. Use a `sync.Once` or a `Done` channel to coordinate.

**Backpressure:** Channel capacity controls backpressure. A buffered channel of size 10 means up to 10 messages can queue before the sender blocks. An unbuffered channel (size 0) means every send blocks until a receive happens. The book's blocking queue with 0 capacity is exactly an unbuffered Go channel.

**Gotcha — closing channels:** Only the sender should close a channel. Sending to a closed channel panics. Design your goroutines so ownership is clear.

#### Reading Resource
[Go Blog — Share Memory By Communicating](https://go.dev/blog/codewalks/sharemem) — the foundational philosophy behind Go's channel-based concurrency.

#### YouTube Search
`"golang channels concurrency patterns websocket server"`

#### Where You'll See This Again
1. **Chat servers:** Every chat application has this exact architecture: a read loop, a write loop, and application logic passing messages through channels.
2. **Event-driven microservices:** Services that consume from message queues (Kafka, NATS) and produce to others use the same goroutine-per-direction pattern with channels for internal routing.
3. **Multiplayer game servers:** Game loops send state updates to connected players through write channels, while read loops consume player actions from the network.

This is the capstone. You're combining everything — TCP sockets, HTTP parsing, protocol upgrades, binary framing, and concurrent programming — into a real-time bidirectional messaging system.

---

## Summary: What You'll Have Built

After completing all six parts, your Go HTTP server supports:

- TCP connection handling with goroutine-per-connection
- HTTP/1.1 request parsing (method, URI, headers, body)
- Response generation with `Content-Length` and `Transfer-Encoding: chunked`
- Keep-alive connections with pipelining support
- Static file serving with `sendfile` optimization
- Range requests (resumable downloads, video seeking)
- Cache validation with `Last-Modified`/`If-Modified-Since`
- Gzip compression with streaming via `io.Pipe`
- WebSocket upgrade, binary frame parsing, and concurrent messaging

And you'll have deeply internalized the concepts of: byte streams vs messages, protocol framing, buffered IO, resource lifecycle management, the producer-consumer pattern, and Go's `io.Reader`/`io.Writer` composition model.
