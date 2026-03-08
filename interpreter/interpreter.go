package interpreter

import (
	"fmt"
	"lux/ast"
	"math"
	"strings"
)

// RuntimeError carries an error with a source line number.
type RuntimeError struct {
	Line    int
	Message string
}

func (e *RuntimeError) Error() string {
	return fmt.Sprintf("runtime error (line %d): %s", e.Line, e.Message)
}

// Interpreter walks the AST and executes it.
type Interpreter struct {
	env    map[string]interface{}
	output strings.Builder // collects print output; flushed to stdout via caller
}

// New creates a fresh interpreter.
func New() *Interpreter {
	return &Interpreter{env: make(map[string]interface{})}
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
	case *ast.PrintStmt:
		return interp.execPrint(s)
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
	interp.env[s.Name] = val
	return nil
}

func (interp *Interpreter) execAssign(s *ast.AssignStmt) error {
	if _, ok := interp.env[s.Name]; !ok {
		return &RuntimeError{Line: s.Line, Message: fmt.Sprintf("undefined variable %q", s.Name)}
	}
	val, err := interp.evalExpr(s.Value)
	if err != nil {
		return err
	}
	interp.env[s.Name] = val
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
	// Save any pre-existing binding for the loop variable.
	old, hadOld := interp.env[s.Var]
	for i := int64(0); i < limit; i++ {
		interp.env[s.Var] = i
		if err := interp.execBlock(s.Body); err != nil {
			return err
		}
	}
	if hadOld {
		interp.env[s.Var] = old
	} else {
		delete(interp.env, s.Var)
	}
	return nil
}

func (interp *Interpreter) execPrint(s *ast.PrintStmt) error {
	val, err := interp.evalExpr(s.Value)
	if err != nil {
		return err
	}
	fmt.Println(valueToString(val))
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
	default:
		return nil, fmt.Errorf("unknown expression type %T", expr)
	}
}

func (interp *Interpreter) evalIdent(e *ast.IdentExpr) (interface{}, error) {
	val, ok := interp.env[e.Name]
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

// toNumbers coerces both values to float64 for comparison/arithmetic.
// Returns isFloat=true if either operand was a float64.
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
	case nil:
		return "nil"
	}
	return fmt.Sprintf("%v", val)
}
