# 🧠 logmachine (Go)

> Collaborative, beautiful logging system for distributed developers — Go edition

**logmachine** brings the full `logmachine` experience to Go. It is built on top of Go's standard `log/slog` library (Go 1.21+) and provides colored terminal output, structured file logging, a custom `SUCCESS` level, dynamic custom levels, log parsing/export, and optional forwarding to a central server via HTTP or Socket.IO WebSocket.

---

## 🚀 Features

- 🔥 **Color-coded terminal logs** (DEBUG, INFO, WARN, ERROR, SUCCESS)
- 📤 **Log forwarding** to a central HTTP or Socket.IO server
- 🪵 **Custom log levels** (add your own with `.NewLevel(...)`)
- 👥 **User identity tracking** for team-based logs
- 🧩 **Pluggable backends**: central server or local files
- 📦 **Simple JSON output** for web dashboards or collectors
- 🧽 Strips ANSI escape codes from logs for clean parsing
- 🧠 Automatically resolves usernames and saves them in `~/.cl_username`
- ✅ Powered by Go's built-in `log/slog` structured logging

---

## ⚙️ Installation

```bash
go get github.com/bufferpunk/logmachine
```

---

## 🧰 Usage

### Basic Setup

```go
package main

import (
    "log/slog"
    logmachine "github.com/bufferpunk/logmachine"
)

func main() {
    logger, err := logmachine.New(logmachine.Options{
        LogFile:    "logs.log",
        ErrorFile:  "errors.log",
        DebugLevel: 0,
    })
    if err != nil {
        panic(err)
    }
    defer logger.Close()

    logger.Info("Hello, world!")
    logger.Error("An error occurred!")
    logger.Success("Operation completed successfully!")
    logger.Debug("Debugging information here.")
    logger.Warn("This is a warning message.")
}
```

### With Central Logging (HTTP)

```go
logger, err := logmachine.New(logmachine.Options{
    LogFile:   "logs.log",
    ErrorFile: "errors.log",
    Central: &logmachine.CentralConfig{
        URL:      "https://logmachine.bufferpunk.com",
        Room:     "team_alpha",
        Endpoint: "/api/logs",                              // optional, default: /api/logs
        Headers:  map[string]string{"Authorization": "Bearer token"},
    },
})
```

### With Central Logging (Socket.IO WebSocket)

```go
logger, err := logmachine.New(logmachine.Options{
    LogFile:  "logs.log",
    ErrorFile: "errors.log",
    Central: &logmachine.CentralConfig{
        URL:          "https://logmachine.bufferpunk.com",
        Room:         "team_alpha",
        SocketIO:     true,
        SocketIOPath: "/api/socket.io/",
    },
})
```

---

## 🎨 Log Format

Every log includes:

* ✅ Username (resolved automatically or via server)
* 📁 Module directory (caller's package directory)
* ⏱️ Timestamp
* 📦 Level (INFO, WARN, ERROR, SUCCESS, ...)
* 📝 Message

Sample (terminal):

```
(username @ myapp) 🤌 CL Timing: [ 2025-08-04T11:23:52+00:00 ]
[ INFO ] Server started on port 8000
🏁
```

---

## 🛠️ Advanced

### Add Your Own Log Level

```go
criticalHack := logger.NewLevel("CRITICAL_HACK", slog.Level(16))
criticalHack("Zero day found!")
```

---

## 📤 Parse & Export

### Convert Logs to JSON

```go
entries, err := logger.Jsonifier()
for _, entry := range entries {
    fmt.Println(entry)
}
```

### Parse a Single Log Entry

```go
entry := logger.ParseLog(rawLogText)
if entry != nil {
    fmt.Println(entry.User, entry.Level, entry.Message)
}
```

---

## 📡 Central Server Compatibility

To use Socket.IO transport, your server must support these events:

* `log` event: receives `{ room: string, data: object }`
* `GET /api/get_username?base=localname`: returns `{ "username": "..." }`

---

## 🤖 Environment Variables

* `CL_USERNAME`: Manually override the detected username
* Automatically stored in `~/.cl_username` for persistent identity

---

## 🔐 Security

* HTTP headers (e.g. `Authorization`) can be injected via `CentralConfig.Headers`
* Central log transmission is fully customizable

---

## 🔧 Configuration Reference

### `Options`

| Field        | Type            | Description                                           |
|--------------|-----------------|-------------------------------------------------------|
| `LogFile`    | `string`        | Path to the general log file (default: `logs.log`)    |
| `ErrorFile`  | `string`        | Path to the error log file (default: `errors.log`)    |
| `DebugLevel` | `int`           | Console output filter (0 = all, 1–7 = filtered)       |
| `Verbose`    | `bool`          | Force all levels through, regardless of DebugLevel    |
| `Central`    | `*CentralConfig`| If set, enables log forwarding to a remote server     |
| `Attached`   | `bool`          | Use Socket.IO instead of HTTP when Central is set     |

### `CentralConfig`

| Field          | Type              | Description                                               |
|----------------|-------------------|-----------------------------------------------------------|
| `URL`          | `string`          | Central server base URL                                   |
| `Room`         | `string`          | Logical group or org name                                 |
| `Endpoint`     | `string`          | HTTP path for POST logs (default: `/api/logs`)            |
| `Headers`      | `map[string]string` | Extra headers to send (e.g. auth token)                 |
| `SocketIO`     | `bool`            | Use Socket.IO WebSocket instead of HTTP                   |
| `SocketIOPath` | `string`          | Path to socket.io on the server (default: `/socket.io/`) |

### DebugLevel Filter Map

| Level | Allowed Levels              |
|-------|-----------------------------|
| `0`   | All levels                  |
| `1`   | ERROR                       |
| `2`   | SUCCESS                     |
| `3`   | WARN                        |
| `4`   | INFO                        |
| `5`   | ERROR, WARN                 |
| `6`   | INFO, SUCCESS               |
| `7`   | ERROR, WARN, INFO           |

---

## 🏃 Running the Example

```bash
cd golang
go run ./example
```

---

## 🧪 Running Tests

```bash
cd golang
go test ./...
```

---

## 📄 License

MIT License

---

## 🙋‍♂️ Author

Mugabo Gusenga  
[logmachine.bufferpunk.com](https://logmachine.bufferpunk.com)

---

## ❤️ Contribute

PRs and issues are welcome!
This tool is built for devs who want **beautiful logs with distributed brains**.
