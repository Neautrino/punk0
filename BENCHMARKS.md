# Performance Benchmarks & Methodology

## 1. Requirements

- Go 1.25+: To build the target binary.
- Git: Required only for `--clone`.
- System Utilities: `curl`, `awk`, `find`, `du`, `python3` (standard on Linux and macOS).
- Disk Space: ~3 GB for the standard multi-repository corpus.
- Memory Measurements: Cross-platform native support (Linux via `/proc` and macOS/Darwin via `getrusage`, `ps`, and `/api/metrics`).

## 2. Running Benchmarks

### Fetch the standard corpus

Clones shallow copies (`--depth 1`) of seven diverse open-source repositories:

```bash
./benchmark.sh --clone
```

### Execute benchmark suite

Spawns an isolated punk0 server process per repository, records metrics, and terminates the instance (`benchmark.sh` still defaults to `BIN=./px0`, so pass the binary):

```bash
BIN=./punk0 ./benchmark.sh
```

### Advanced Modes & Exports

```bash
# Export benchmark results as machine-readable JSON or CSV
./benchmark.sh --json baseline.json
./benchmark.sh --csv results.csv

# Compare benchmark against a saved baseline (speedups, regressions, memory delta)
./benchmark.sh --compare baseline.json

# Run Go micro-benchmarks (fuzzy search, syntax highlighting, regex/literal search, allocs/op)
./benchmark.sh --micro
# or: make bench

# Measure concurrent HTTP load throughput and latency distribution (p50, p95, p99)
./benchmark.sh --load .
```

## 3. Benchmark Corpus

The seven repositories were chosen to span two orders of magnitude in size and represent diverse language ecosystems:

| Repository                                            | Primary Language | Character / Role in Benchmark                                  |
| ----------------------------------------------------- | ---------------- | -------------------------------------------------------------- |
| [flask](https://github.com/pallets/flask)             | Python           | Compact library: tests instant sub-millisecond path.           |
| [redis](https://github.com/redis/redis)               | C                | Medium-sized C codebase with large monolithic source files.    |
| [react](https://github.com/facebook/react)             | JavaScript       | Deep directory nesting and many nested `.gitignore` files.     |
| [django](https://github.com/django/django)           | Python           | Large framework with thousands of modules and test suites.     |
| [TypeScript](https://github.com/microsoft/TypeScript) | TypeScript       | Very large source files with a massive generated baseline tree.|
| [kubernetes](https://github.com/kubernetes/kubernetes)| Go               | Large Go monorepo with extensive vendored code.                |
| [linux](https://github.com/torvalds/linux)            | C                | The extreme case: ~95,000 files and 1.8 GB of source text.     |

## 4. Benchmark Results

px0's measurements (not re-measured for punk0), on Linux x86_64 with language servers disabled (`-no-lsp`):

| Repo       | Source Size | Files  | Index  | Fuzzy   | Full Scan | Open Big | Reopen | Base Mem | Peak Mem |
| ---------- | ----------- | ------ | ------ | ------- | --------- | -------- | ------ | -------- | -------- |
| django     | 74 MB       | 7,014  | 39 ms  | 1.3 ms  | 26.8 ms   | 166.8 ms | 1.0 ms | 20 MB    | 29 MB    |
| flask      | 3 MB        | 235    | 1 ms   | 0.8 ms  | 2.3 ms    | n/a      | n/a    | 16 MB    | 18 MB    |
| kubernetes | 370 MB      | 25,926 | 150 ms | 13.5 ms | 84.6 ms   | 199.0 ms | 0.9 ms | 30 MB    | 44 MB    |
| linux      | 1,809 MB    | 95,710 | 370 ms | 6.0 ms  | 451.8 ms  | 26.7 ms  | 0.6 ms | 55 MB    | 73 MB    |
| react      | 63 MB       | 7,178  | 52 ms  | 2.7 ms  | 32.2 ms   | 57.7 ms  | 0.7 ms | 21 MB    | 28 MB    |
| redis      | 26 MB       | 1,855  | 13 ms  | 1.0 ms  | 18.2 ms   | 80.8 ms  | 1.2 ms | 17 MB    | 27 MB    |
| typescript | 414 MB      | 66,533 | 566 ms | 6.2 ms  | 150.3 ms  | 40.9 ms  | 6.5 ms | 69 MB    | 105 MB   |

### 4.1 Benchmark Results (macOS arm64 / Apple Silicon)

px0's measurements (not re-measured for punk0), on macOS (Apple M4, Darwin arm64) with language servers disabled (`-no-lsp`):

| Repo       | Source Size | Files  | Index   | Fuzzy   | Full Scan  | Open Big  | Reopen   | Base Mem | Peak Mem |
| ---------- | ----------- | ------ | ------- | ------- | ---------- | --------- | -------- | -------- | -------- |
| django     | 61 MB       | 7,014  | 142 ms  | 0.6 ms  | 50.7 ms    | 108.1 ms  | 8.4 ms   | 28 MB    | 46 MB    |
| flask      | 2 MB        | 235    | 19 ms   | 0.4 ms  | 2.6 ms     | n/a       | n/a      | 23 MB    | 27 MB    |
| kubernetes | 346 MB      | 25,922 | 544 ms  | 3.7 ms  | 299.9 ms   | 89.5 ms   | 8.4 ms   | 40 MB    | 110 MB   |
| linux      | 1,783 MB    | 95,705 | 981 ms  | 4.2 ms  | 2,208.5 ms | 71.5 ms   | 11.9 ms  | 73 MB    | 367 MB   |
| react      | 60 MB       | 7,204  | 102 ms  | 1.3 ms  | 66.3 ms    | 38.6 ms   | 7.4 ms   | 27 MB    | 48 MB    |
| redis      | 25 MB       | 1,866  | 31 ms   | 0.6 ms  | 14.2 ms    | 32.8 ms   | 6.4 ms   | 25 MB    | 43 MB    |
| typescript | 398 MB      | 66,617 | 925 ms  | 4.1 ms  | 1,019.2 ms | 52.0 ms   | 12.8 ms  | 77 MB    | 125 MB   |


- `Source`: Total working tree size (excluding `.git`).
- `Files`: Number of indexed files after applying `.gitignore` and built-in rules.
- `Index`: Cold startup directory walk and ignore set construction time.
- `Fuzzy`: Time to fuzzy-match a query across every indexed path.
- `Full Scan`: Literal search for a term that matches nothing (worst-case full codebase scan reading every byte).
- `Open Big`: Cold open of the largest source file: read, tokenize viewport window, return HTML.
- `Reopen`: Opening the same file once cached in memory.
- `Base Mem`: Resident memory (RSS) after indexing.
- `Peak Mem`: Peak memory during aggressive search and navigation prior to idle scavenging.

## 5. Memory Scavenging Verification

To observe resident memory scavenging in real time, run:

```bash
./benchmark.sh --memory bench-repos/linux
```

Output measured on px0 (not re-measured for punk0):

```text
### linux
  after indexing                     59 MB
  after a fuzzy find                 59 MB
  after 5 full-tree searches         85 MB
  after opening the largest file     88 MB
  after scrolling through it         94 MB
  8 seconds idle                     94 MB
  30 seconds idle                    57 MB
```

After 15 seconds of inactivity, punk0 triggers `debug.FreeOSMemory()`, returning unused heap pages back to the Linux kernel and settling back to baseline.
