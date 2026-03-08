# Lux

A small, hand-rolled interpreted programming language written in Go — with no external dependencies.

Lux has a Python-ish syntax and is built on the classic interpreter stack: lexer → parser → AST → tree-walk interpreter. It runs as a file executor or an interactive REPL.

---

## Table of Contents

- [Language Tour](#language-tour)
- [Running Lux](#running-lux)
- [Architecture](#architecture)
  - [Lexer](#lexer)
  - [AST](#ast)
  - [Parser](#parser)
  - [Interpreter](#interpreter)
  - [Entry Point](#entry-point)
- [Examples](#examples)

---

## Language Tour

### Variables

```lux
let x = 42
let name = "Lux"
let pi = 3.14
let flag = true
```

`let` declares a new variable. Plain `=` reassigns an existing one — using `=` on an undeclared variable is a runtime error.

```lux
let count = 0
count = count + 1
```

### Types

| Type | Example |
|------|---------|
| Integer | `42`, `-7` |
| Float | `3.14`, `-0.5` |
| String | `"hello"`, `"line\nbreak"` |
| Boolean | `true`, `false` |

Integers and floats are auto-promoted: `3 + 4` stays `int`, but `3 + 4.0` becomes `float`.

### Arithmetic

```lux
let a = 10 + 3   # 13
let b = 10 - 3   # 7
let c = 10 * 3   # 30
let d = 10 / 3   # 3  (integer division)
let e = 10 / 3.0 # 3.3333...
let f = 10 % 3   # 1
```

### Comparison

```lux
x == y    # equal
x != y    # not equal
x < y
x > y
x <= y
x >= y
```

### Logic

```lux
a && b    # and  (short-circuits)
a || b    # or   (short-circuits)
!a        # not
```

### Strings

```lux
let greeting = "Hello, " + "World!"
```

Supported escape sequences: `\n`, `\t`, `\"`, `\\`.

### Conditionals

```lux
if score >= 90 {
    print("Grade: A")
} else {
    if score >= 75 {
        print("Grade: B")
    } else {
        print("Grade: C")
    }
}
```

`else if` is written as a nested `if` inside `else { }`.

### Loops

```lux
for i in range(10) {
    print(i)    # prints 0 through 9
}
```

`range(n)` iterates from `0` to `n-1`. The loop variable is scoped to the loop — any outer variable with the same name is restored after the loop exits.

### Output

```lux
print("Hello, World!")
print(42 * 2)
print("Result: " + "done")
```

### Comments

```lux
# This is a comment
let x = 1   # inline comment
```

---

## Running Lux

**Run a file:**

```bash
go run . examples/hello.lux
```

**Start the REPL:**

```bash
go run .
```

```
Lux REPL  (type 'exit' or Ctrl-D to quit)
>>> let x = 10
>>> x = x + 5
>>> print(x)
15
```

The REPL maintains state across inputs. Multi-line blocks (anything with unmatched `{`) keep prompting with `...` until all braces are closed:

```
>>> for i in range(3) {
...     print(i)
... }
0
1
2
```

---

## Architecture

The execution pipeline is:

```
source text (.lux)
      │
      ▼
  Lexer          tokenizes characters into a flat token list
      │
      ▼
  Parser         consumes tokens, builds an AST
      │
      ▼
  AST            in-memory tree of statement and expression nodes
      │
      ▼
  Interpreter    walks the AST and executes it
```

### Lexer

**File:** `lexer/lexer.go`

The lexer is a single linear scan over the UTF-8 source string. It produces a `[]Token` slice that the parser consumes.

Each `Token` carries three fields:

```go
type Token struct {
    Type    TokenType  // what kind of token
    Literal string     // raw text
    Line    int        // source line (for error messages)
}
```

**Dispatch logic** — at each character position the lexer does:

| Character | Action |
|-----------|--------|
| `\n` | Emit `NEWLINE` if previous token was meaningful; increment line counter |
| `#` | Skip to end of line (comment) |
| `"` | Read string literal with escape handling |
| digit | Read integer or float (`1.23` → `FLOAT`, `42` → `INT`) |
| letter / `_` | Read identifier, then check keyword map |
| everything else | Read 1- or 2-character operator symbol |

**Smart NEWLINE emission** — consecutive NEWLINEs and NEWLINEs immediately after `{` are suppressed. This lets the parser use `NEWLINE` as a statement terminator without tripping over blank lines or block-opening braces.

**Keyword map** — all keywords (`let`, `if`, `else`, `for`, `in`, `range`, `print`, `true`, `false`) are stored in a `map[string]TokenType`. Every identifier is read first as a raw string, then looked up. Unknown names become `IDENT`.

**Operator handling** — `readSymbol()` peeks one character ahead to distinguish single- from double-character operators:
- `=` vs `==`
- `!` vs `!=`
- `<` vs `<=`
- `>` vs `>=`
- `&&`, `||`

**String escape sequences** — `readString()` processes `\n`, `\t`, `\"`, `\\` as it scans. An unterminated string returns a lex error with the line number.

**Number parsing** — `readNumber()` detects floats by watching for a `.`. A second `.` stops the scan (not an error at lex time).

---

### AST

**File:** `ast/ast.go`

The AST is the in-memory tree representation of a parsed program. All nodes implement the `Node` interface (which requires `node()` and `String()`). Nodes are split into two sub-interfaces:

- **`Statement`** — something that *does* something
- **`Expression`** — something that *produces* a value

**Statement nodes:**

| Node | Syntax |
|------|--------|
| `LetStmt` | `let name = expr` |
| `AssignStmt` | `name = expr` |
| `IfStmt` | `if expr { } else { }` — holds condition + two `[]Statement` slices |
| `ForRangeStmt` | `for var in range(expr) { }` — holds var name, limit expr, body slice |
| `PrintStmt` | `print(expr)` |
| `ExprStmt` | a bare expression used as a statement |

**Expression nodes:**

| Node | Represents |
|------|------------|
| `NumberLit` | integer or float — carries both `IntVal int64` and `FloatVal float64` with an `IsFloat` flag |
| `StringLit` | string value (already unescaped by the lexer) |
| `BoolLit` | `true` or `false` |
| `IdentExpr` | a variable name — resolved at runtime |
| `BinaryExpr` | `left op right` — operator stored as a string |
| `UnaryExpr` | `!x` or `-x` |

Every node stores a `Line` field so the interpreter can report errors with source line numbers.

---

### Parser

**File:** `parser/parser.go`

The parser uses **recursive descent** for statements and **Pratt / precedence climbing** for expressions.

**State** — two fields: the token slice and an integer position cursor `pos`. `peek()` reads without consuming; `advance()` consumes and returns.

#### Statement parsing

`parseStatement()` dispatches on the current token:

```
LET    → parseLetStmt()         let <IDENT> = <expr>
IF     → parseIfStmt()          if <expr> <block> [else <block>]
FOR    → parseForRangeStmt()    for <IDENT> in range(<expr>) <block>
PRINT  → parsePrintStmt()       print(<expr>)
IDENT  → peek ahead 1:
           next is '='  → parseAssignStmt()
           otherwise    → parseExprStmt()
_      → parseExprStmt()
```

`parseBlock()` reads `{ stmt* }`, skipping newlines freely inside the braces.

`expectNewlineOrEOF()` enforces statement termination — every single-line statement must end with a newline, `}`, or EOF.

#### Expression parsing — Pratt / precedence climbing

`parseExpr(minPrec)`:

1. Parse the left-hand side via `parsePrimary()` (atoms: literals, identifiers, unary ops, grouped expressions)
2. Loop: peek at the next token's infix precedence. If it is ≥ `minPrec`, consume the operator, recursively call `parseExpr(prec + 1)` for the right-hand side, wrap in `BinaryExpr`, continue.

Operator precedence table (higher = tighter binding, all left-associative):

| Precedence | Operators |
|-----------|-----------|
| 6 (tightest) | `*` `/` `%` |
| 5 | `+` `-` |
| 4 | `<` `>` `<=` `>=` |
| 3 | `==` `!=` |
| 2 | `&&` |
| 1 (loosest) | `\|\|` |

So `a || b && c == d + e * f` parses as `a || (b && (c == (d + (e * f))))`.

`parsePrimary()` handles:
- Number / string / bool literals → leaf nodes
- Identifiers → `IdentExpr`
- `!` or `-` → recurse into `parsePrimary()` (unary, right-associative by nature)
- `(` → recurse into `parseExpr(0)`, then expect `)`

---

### Interpreter

**File:** `interpreter/interpreter.go`

A **tree-walk interpreter** — it walks the AST and executes nodes directly, with no compilation step. The runtime environment is a flat `map[string]interface{}`.

Concrete Go types used as Lux values: `int64`, `float64`, `string`, `bool`.

#### Statement execution

| Statement | Behaviour |
|-----------|-----------|
| `LetStmt` | Evaluates expr, stores result in `env[name]` |
| `AssignStmt` | Checks the variable exists (runtime error if not), then updates it |
| `IfStmt` | Evaluates condition → `toBool()` → executes consequence or alternative |
| `ForRangeStmt` | Evaluates limit → `toInt()` → loops `i = 0..limit-1`; saves and restores any pre-existing binding for the loop variable |
| `PrintStmt` | Evaluates expr → `valueToString()` → `fmt.Println` |

#### Expression evaluation

Binary operators are handled in three layers:

1. **Short-circuit logic** — `&&` returns `false` immediately if left is falsy; `||` returns `true` immediately if left is truthy.
2. **Equality** — `==` and `!=` use Go's `==` directly, works across all types.
3. **Arithmetic and comparison** — both operands are coerced to `float64` via `toNumbers()`. If neither was originally a float, the result is cast back to `int64`.

String `+` is handled before numeric promotion — if both sides are strings it concatenates them.

#### Type coercion

`toBool(val)`:
- `bool` → as-is
- `int64` / `float64` → `!= 0`
- `string` → `!= ""`

`toNumbers(left, right)` — promotes both to `float64`; sets `isFloat = true` if either was originally a `float64`. This preserves integer arithmetic: `3 + 4 → int64(7)`, but `3 + 4.0 → float64(7)`.

#### Loop variable scoping

Before a `for` loop the interpreter saves any existing binding for the loop variable, and restores it (or deletes it) after the loop exits:

```lux
let i = 99
for i in range(3) { print(i) }  # 0, 1, 2
print(i)                          # 99
```

#### Error handling

Runtime errors are wrapped in `RuntimeError{Line, Message}` which formats as `runtime error (line N): message`. The interpreter also wraps a `recover()` around the top-level run to catch any unexpected panics.

---

### Entry Point

**File:** `main.go`

Two execution modes:

**File mode** — `go run . script.lux`

Reads the file, creates a fresh interpreter, and runs it. Any error is printed to stderr and the process exits with code 1.

**REPL mode** — `go run .`

Creates one persistent `Interpreter` that lives for the entire session (variables declared in one line are available in the next). Uses `needsMore()` to detect unclosed `{` blocks — prompts with `...` until all braces balance, then submits the whole block:

```
>>> if x > 0 {
...     print("positive")
... }
```

Type `exit` or press `Ctrl-D` to quit.

The full pipeline per execution:

```
source string
  → lexer.New(src)      → []Token
  → parser.New(tokens)  → *ast.Program
  → interp.Run(prog)    → output + env mutations
```

---

## Examples

**`examples/hello.lux`** — string variables and arithmetic

```lux
let name = "World"
print("Hello, " + name + "!")

let x = 6
let y = 7
print(x * y)
```

**`examples/booleans.lux`** — boolean logic and nested conditionals

```lux
let a = true
let b = false

if a && !b {
    print("a is true and b is false")
}

let score = 85
if score >= 90 {
    print("Grade: A")
} else {
    if score >= 75 {
        print("Grade: B")
    } else {
        print("Grade: C")
    }
}
```

**`examples/loop.lux`** — range loop and post-loop conditional

```lux
let sum = 0
for i in range(10) {
    sum = sum + i
}
print(sum)   # 45

if sum > 40 {
    print("That's a big sum!")
}
```
