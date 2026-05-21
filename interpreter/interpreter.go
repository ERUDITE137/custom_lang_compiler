package interpreter

import (
	"fmt"
	"lux/ast"
	"math"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

// RuntimeError carries an error with a source line number.
type RuntimeError struct {
	Line    int
	Message string
}

func (e *RuntimeError) Error() string {
	return fmt.Sprintf("runtime error (line %d): %s", e.Line, e.Message)
}

// ─── STM runtime types ──────────────────────────────────────────────────────

// sentinel for deleted variables inside a transaction
type tombstone struct{}

// TxnLog holds the read-set, write-set, and buffered I/O for one transaction.
type TxnLog struct {
	readSet  map[string]interface{}
	writeSet map[string]interface{}
	ioBuf    strings.Builder // buffered print output; flushed only on commit
}

// errRetry is a sentinel error used by the retry statement.
var errRetry = fmt.Errorf("STM retry")

// LuxThread is a handle to a spawned goroutine.
type LuxThread struct {
	done chan struct{}
	err  error
}

// Interpreter walks the AST and executes it.
type Interpreter struct {
	env        map[string]interface{}
	mu         sync.Mutex   // protects env; also used as commitCond's locker
	commitCond *sync.Cond   // broadcast on every successful STM commit
	txns       sync.Map     // goroutineID → *TxnLog (active transactions)
	output     strings.Builder
}

// New creates a fresh interpreter.
func New() *Interpreter {
	interp := &Interpreter{env: make(map[string]interface{})}
	interp.commitCond = sync.NewCond(&interp.mu)
	return interp
}

// ─── goroutine ID helper ────────────────────────────────────────────────────

func goroutineID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	// format: "goroutine 123 [...]"
	s := string(buf[:n])
	s = s[len("goroutine "):]
	idx := 0
	for idx < len(s) && s[idx] >= '0' && s[idx] <= '9' {
		idx++
	}
	id, _ := strconv.ParseUint(s[:idx], 10, 64)
	return id
}

// activeTxn returns the current goroutine's transaction log, or nil.
func (interp *Interpreter) activeTxn() *TxnLog {
	if v, ok := interp.txns.Load(goroutineID()); ok {
		return v.(*TxnLog)
	}
	return nil
}

// ─── thread-safe env accessors (transaction-aware) ──────────────────────────

func (interp *Interpreter) getVar(name string) (interface{}, bool) {
	if txn := interp.activeTxn(); txn != nil {
		// Check write-set first (our own pending writes)
		if v, ok := txn.writeSet[name]; ok {
			if _, dead := v.(tombstone); dead {
				return nil, false
			}
			return v, true
		}
		// Check read-set (already read in this txn)
		if v, ok := txn.readSet[name]; ok {
			return v, true
		}
		// Read from shared env and record in read-set
		interp.mu.Lock()
		v, ok := interp.env[name]
		interp.mu.Unlock()
		if ok {
			txn.readSet[name] = v
		}
		return v, ok
	}
	interp.mu.Lock()
	defer interp.mu.Unlock()
	v, ok := interp.env[name]
	return v, ok
}

func (interp *Interpreter) setVar(name string, val interface{}) {
	if txn := interp.activeTxn(); txn != nil {
		txn.writeSet[name] = val
		return
	}
	interp.mu.Lock()
	defer interp.mu.Unlock()
	interp.env[name] = val
}

func (interp *Interpreter) deleteVar(name string) {
	if txn := interp.activeTxn(); txn != nil {
		txn.writeSet[name] = tombstone{}
		return
	}
	interp.mu.Lock()
	defer interp.mu.Unlock()
	delete(interp.env, name)
}

// ─── STM commit logic ───────────────────────────────────────────────────────

// validateTxn checks that every value in the read-set still matches shared env.
// Caller must hold interp.mu.
func (interp *Interpreter) validateTxn(txn *TxnLog) bool {
	for name, expected := range txn.readSet {
		actual, ok := interp.env[name]
		if !ok || actual != expected {
			return false
		}
	}
	return true
}

// commitTxn attempts to validate and commit a transaction.
// Returns true on success, false on conflict.
func (interp *Interpreter) commitTxn(txn *TxnLog) bool {
	interp.mu.Lock()
	defer interp.mu.Unlock()
	if !interp.validateTxn(txn) {
		return false
	}
	for name, val := range txn.writeSet {
		if _, dead := val.(tombstone); dead {
			delete(interp.env, name)
		} else {
			interp.env[name] = val
		}
	}
	// Wake up any transactions blocked on retry
	interp.commitCond.Broadcast()
	return true
}

// Run executes a program and returns any runtime error.
func (interp *Interpreter) Run(prog *ast.Program) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	for _, stmt := range prog.Statements {
		if e := interp.execStmt(stmt); e != nil {
			return e
		}
	}
	return nil
}

// ─── statements ──────────────────────────────────────────────────────────────

func (interp *Interpreter) execStmt(stmt ast.Statement) error {
	switch s := stmt.(type) {
	case *ast.LetStmt:
		return interp.execLet(s)
	case *ast.AssignStmt:
		return interp.execAssign(s)
	case *ast.IfStmt:
		return interp.execIf(s)
	case *ast.ForRangeStmt:
		return interp.execForRange(s)
	case *ast.WhileStmt:
		return interp.execWhile(s)
	case *ast.PrintStmt:
		return interp.execPrint(s)
	case *ast.AtomicStmt:
		return interp.execAtomic(s)
	case *ast.RetryStmt:
		return interp.execRetry(s)
	case *ast.JoinStmt:
		return interp.execJoin(s)
	case *ast.ExprStmt:
		_, err := interp.evalExpr(s.Expression)
		return err
	default:
		return fmt.Errorf("unknown statement type %T", stmt)
	}
}

func (interp *Interpreter) execLet(s *ast.LetStmt) error {
	val, err := interp.evalExpr(s.Value)
	if err != nil {
		return err
	}
	interp.setVar(s.Name, val)
	return nil
}

func (interp *Interpreter) execAssign(s *ast.AssignStmt) error {
	if _, ok := interp.getVar(s.Name); !ok {
		return &RuntimeError{Line: s.Line, Message: fmt.Sprintf("undefined variable %q", s.Name)}
	}
	val, err := interp.evalExpr(s.Value)
	if err != nil {
		return err
	}
	interp.setVar(s.Name, val)
	return nil
}

func (interp *Interpreter) execIf(s *ast.IfStmt) error {
	cond, err := interp.evalExpr(s.Condition)
	if err != nil {
		return err
	}
	b, err := toBool(cond, s.Line)
	if err != nil {
		return err
	}
	if b {
		return interp.execBlock(s.Consequence)
	}
	return interp.execBlock(s.Alternative)
}

func (interp *Interpreter) execForRange(s *ast.ForRangeStmt) error {
	limitVal, err := interp.evalExpr(s.Limit)
	if err != nil {
		return err
	}
	limit, err := toInt(limitVal, s.Line)
	if err != nil {
		return err
	}
	old, hadOld := interp.getVar(s.Var)
	for i := int64(0); i < limit; i++ {
		interp.setVar(s.Var, i)
		if err := interp.execBlock(s.Body); err != nil {
			return err
		}
	}
	if hadOld {
		interp.setVar(s.Var, old)
	} else {
		interp.deleteVar(s.Var)
	}
	return nil
}

func (interp *Interpreter) execWhile(s *ast.WhileStmt) error {
	for {
		cond, err := interp.evalExpr(s.Condition)
		if err != nil {
			return err
		}
		b, err := toBool(cond, s.Line)
		if err != nil {
			return err
		}
		if !b {
			break
		}
		if err := interp.execBlock(s.Body); err != nil {
			return err
		}
	}
	return nil
}

func (interp *Interpreter) execPrint(s *ast.PrintStmt) error {
	val, err := interp.evalExpr(s.Value)
	if err != nil {
		return err
	}
	str := valueToString(val)
	// Inside a transaction, buffer output — only flush on commit
	if txn := interp.activeTxn(); txn != nil {
		txn.ioBuf.WriteString(str)
		txn.ioBuf.WriteByte('\n')
		return nil
	}
	fmt.Println(str)
	return nil
}

func (interp *Interpreter) execBlock(stmts []ast.Statement) error {
	for _, stmt := range stmts {
		if err := interp.execStmt(stmt); err != nil {
			return err
		}
	}
	return nil
}

// ─── STM: atomic and retry ──────────────────────────────────────────────────

func (interp *Interpreter) execAtomic(s *ast.AtomicStmt) error {
	gid := goroutineID()
	attempt := 0
	for {
		attempt++
		txn := &TxnLog{
			readSet:  make(map[string]interface{}),
			writeSet: make(map[string]interface{}),
		}
		interp.txns.Store(gid, txn)

		err := interp.execBlock(s.Body)

		interp.txns.Delete(gid)

		if err == errRetry {
			// Check if read-set already changed before blocking
			interp.mu.Lock()
			if interp.validateTxn(txn) {
				// Variables haven't changed yet — wait for a commit
				fmt.Printf("[STM] retry: waiting for variables to change (attempt %d)\n", attempt)
				interp.commitCond.Wait()
			}
			interp.mu.Unlock()
			continue
		}
		if err != nil {
			return err
		}

		if interp.commitTxn(txn) {
			// Flush buffered I/O only on successful commit
			if txn.ioBuf.Len() > 0 {
				fmt.Print(txn.ioBuf.String())
			}
			if attempt > 1 {
				fmt.Printf("[STM] transaction committed after %d attempts\n", attempt)
			}
			return nil
		}
		// Validation failed — conflict detected, retry
		fmt.Printf("[STM] conflict detected, retrying... (attempt %d)\n", attempt)
	}
}

func (interp *Interpreter) execRetry(s *ast.RetryStmt) error {
	if interp.activeTxn() == nil {
		return &RuntimeError{Line: s.Line, Message: "retry can only be used inside an atomic block"}
	}
	return errRetry
}

// ─── concurrency: spawn and join ────────────────────────────────────────────

func (interp *Interpreter) execJoin(s *ast.JoinStmt) error {
	val, err := interp.evalExpr(s.Value)
	if err != nil {
		return err
	}
	thread, ok := val.(*LuxThread)
	if !ok {
		return &RuntimeError{Line: s.Line, Message: fmt.Sprintf("join() expects a thread, got %T", val)}
	}
	<-thread.done
	if thread.err != nil {
		return thread.err
	}
	return nil
}

// ─── expressions ─────────────────────────────────────────────────────────────

func (interp *Interpreter) evalExpr(expr ast.Expression) (interface{}, error) {
	switch e := expr.(type) {
	case *ast.NumberLit:
		if e.IsFloat {
			return e.FloatVal, nil
		}
		return e.IntVal, nil
	case *ast.StringLit:
		return e.Value, nil
	case *ast.BoolLit:
		return e.Value, nil
	case *ast.IdentExpr:
		return interp.evalIdent(e)
	case *ast.UnaryExpr:
		return interp.evalUnary(e)
	case *ast.BinaryExpr:
		return interp.evalBinary(e)
	case *ast.SpawnExpr:
		return interp.evalSpawn(e)
	default:
		return nil, fmt.Errorf("unknown expression type %T", expr)
	}
}

func (interp *Interpreter) evalIdent(e *ast.IdentExpr) (interface{}, error) {
	val, ok := interp.getVar(e.Name)
	if !ok {
		return nil, &RuntimeError{Line: e.Line, Message: fmt.Sprintf("undefined variable %q", e.Name)}
	}
	return val, nil
}

func (interp *Interpreter) evalUnary(e *ast.UnaryExpr) (interface{}, error) {
	val, err := interp.evalExpr(e.Operand)
	if err != nil {
		return nil, err
	}
	switch e.Op {
	case "!":
		b, err := toBool(val, e.Line)
		if err != nil {
			return nil, err
		}
		return !b, nil
	case "-":
		switch v := val.(type) {
		case int64:
			return -v, nil
		case float64:
			return -v, nil
		}
		return nil, &RuntimeError{Line: e.Line, Message: "unary - requires a number"}
	}
	return nil, &RuntimeError{Line: e.Line, Message: fmt.Sprintf("unknown unary op %q", e.Op)}
}

func (interp *Interpreter) evalBinary(e *ast.BinaryExpr) (interface{}, error) {
	// Short-circuit logical operators
	if e.Op == "&&" {
		left, err := interp.evalExpr(e.Left)
		if err != nil {
			return nil, err
		}
		lb, err := toBool(left, e.Line)
		if err != nil {
			return nil, err
		}
		if !lb {
			return false, nil
		}
		right, err := interp.evalExpr(e.Right)
		if err != nil {
			return nil, err
		}
		return toBool(right, e.Line)
	}
	if e.Op == "||" {
		left, err := interp.evalExpr(e.Left)
		if err != nil {
			return nil, err
		}
		lb, err := toBool(left, e.Line)
		if err != nil {
			return nil, err
		}
		if lb {
			return true, nil
		}
		right, err := interp.evalExpr(e.Right)
		if err != nil {
			return nil, err
		}
		return toBool(right, e.Line)
	}

	left, err := interp.evalExpr(e.Left)
	if err != nil {
		return nil, err
	}
	right, err := interp.evalExpr(e.Right)
	if err != nil {
		return nil, err
	}

	// Equality works on any types
	switch e.Op {
	case "==":
		return left == right, nil
	case "!=":
		return left != right, nil
	}

	// String concatenation
	if e.Op == "+" {
		ls, lok := left.(string)
		rs, rok := right.(string)
		if lok && rok {
			return ls + rs, nil
		}
	}

	// Numeric arithmetic and comparison
	lf, rf, isFloat, err := toNumbers(left, right, e.Line)
	if err != nil {
		return nil, err
	}

	switch e.Op {
	case "+":
		if isFloat {
			return lf + rf, nil
		}
		return int64(lf) + int64(rf), nil
	case "-":
		if isFloat {
			return lf - rf, nil
		}
		return int64(lf) - int64(rf), nil
	case "*":
		if isFloat {
			return lf * rf, nil
		}
		return int64(lf) * int64(rf), nil
	case "/":
		if rf == 0 {
			return nil, &RuntimeError{Line: e.Line, Message: "division by zero"}
		}
		if isFloat {
			return lf / rf, nil
		}
		return int64(lf) / int64(rf), nil
	case "%":
		if rf == 0 {
			return nil, &RuntimeError{Line: e.Line, Message: "modulo by zero"}
		}
		return int64(math.Round(lf)) % int64(math.Round(rf)), nil
	case "<":
		return lf < rf, nil
	case ">":
		return lf > rf, nil
	case "<=":
		return lf <= rf, nil
	case ">=":
		return lf >= rf, nil
	}

	return nil, &RuntimeError{Line: e.Line, Message: fmt.Sprintf("unknown operator %q", e.Op)}
}

func (interp *Interpreter) evalSpawn(e *ast.SpawnExpr) (interface{}, error) {
	thread := &LuxThread{done: make(chan struct{})}
	body := e.Body
	go func() {
		defer close(thread.done)
		if err := interp.execBlock(body); err != nil {
			thread.err = err
		}
	}()
	return thread, nil
}

// ─── type helpers ─────────────────────────────────────────────────────────────

func toBool(val interface{}, line int) (bool, error) {
	switch v := val.(type) {
	case bool:
		return v, nil
	case int64:
		return v != 0, nil
	case float64:
		return v != 0, nil
	case string:
		return v != "", nil
	}
	return false, &RuntimeError{Line: line, Message: fmt.Sprintf("cannot convert %T to bool", val)}
}

func toInt(val interface{}, line int) (int64, error) {
	switch v := val.(type) {
	case int64:
		return v, nil
	case float64:
		return int64(v), nil
	}
	return 0, &RuntimeError{Line: line, Message: fmt.Sprintf("expected integer, got %T", val)}
}

func toNumbers(left, right interface{}, line int) (lf, rf float64, isFloat bool, err error) {
	toF := func(v interface{}) (float64, bool, error) {
		switch n := v.(type) {
		case int64:
			return float64(n), false, nil
		case float64:
			return n, true, nil
		}
		return 0, false, &RuntimeError{Line: line, Message: fmt.Sprintf("expected number, got %T (%v)", v, v)}
	}
	lf, lIsF, err := toF(left)
	if err != nil {
		return 0, 0, false, err
	}
	rf, rIsF, err := toF(right)
	if err != nil {
		return 0, 0, false, err
	}
	return lf, rf, lIsF || rIsF, nil
}

func valueToString(val interface{}) string {
	switch v := val.(type) {
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case int64:
		return fmt.Sprintf("%d", v)
	case float64:
		return fmt.Sprintf("%g", v)
	case *LuxThread:
		return "<thread>"
	case nil:
		return "nil"
	}
	return fmt.Sprintf("%v", val)
}
