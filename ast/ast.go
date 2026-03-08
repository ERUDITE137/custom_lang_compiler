package ast

import "fmt"

// Node is the interface all AST nodes implement.
type Node interface {
	node()
	String() string
}

// Statement nodes
type Statement interface {
	Node
	stmtNode()
}

// Expression nodes
type Expression interface {
	Node
	exprNode()
}

// ─── Program ────────────────────────────────────────────────────────────────

type Program struct {
	Statements []Statement
}

func (p *Program) node()          {}
func (p *Program) String() string { return fmt.Sprintf("Program(%d stmts)", len(p.Statements)) }

// ─── Statements ─────────────────────────────────────────────────────────────

// LetStmt: let <name> = <value>
type LetStmt struct {
	Line  int
	Name  string
	Value Expression
}

func (s *LetStmt) node()          {}
func (s *LetStmt) stmtNode()      {}
func (s *LetStmt) String() string { return fmt.Sprintf("let %s = %s", s.Name, s.Value) }

// AssignStmt: <name> = <value>
type AssignStmt struct {
	Line  int
	Name  string
	Value Expression
}

func (s *AssignStmt) node()          {}
func (s *AssignStmt) stmtNode()      {}
func (s *AssignStmt) String() string { return fmt.Sprintf("%s = %s", s.Name, s.Value) }

// IfStmt: if <cond> { <then> } [else { <else> }]
type IfStmt struct {
	Line        int
	Condition   Expression
	Consequence []Statement
	Alternative []Statement // nil if no else
}

func (s *IfStmt) node()          {}
func (s *IfStmt) stmtNode()      {}
func (s *IfStmt) String() string { return fmt.Sprintf("if %s { ... }", s.Condition) }

// ForRangeStmt: for <var> in range(<limit>) { <body> }
type ForRangeStmt struct {
	Line  int
	Var   string
	Limit Expression
	Body  []Statement
}

func (s *ForRangeStmt) node()          {}
func (s *ForRangeStmt) stmtNode()      {}
func (s *ForRangeStmt) String() string {
	return fmt.Sprintf("for %s in range(%s) { ... }", s.Var, s.Limit)
}

// PrintStmt: print(<expr>)
type PrintStmt struct {
	Line  int
	Value Expression
}

func (s *PrintStmt) node()          {}
func (s *PrintStmt) stmtNode()      {}
func (s *PrintStmt) String() string { return fmt.Sprintf("print(%s)", s.Value) }

// ExprStmt wraps a bare expression used as a statement.
type ExprStmt struct {
	Line       int
	Expression Expression
}

func (s *ExprStmt) node()          {}
func (s *ExprStmt) stmtNode()      {}
func (s *ExprStmt) String() string { return s.Expression.String() }

// ─── Expressions ────────────────────────────────────────────────────────────

// NumberLit holds an integer or float literal.
type NumberLit struct {
	Line    int
	IsFloat bool
	IntVal  int64
	FloatVal float64
}

func (e *NumberLit) node()     {}
func (e *NumberLit) exprNode() {}
func (e *NumberLit) String() string {
	if e.IsFloat {
		return fmt.Sprintf("%g", e.FloatVal)
	}
	return fmt.Sprintf("%d", e.IntVal)
}

// StringLit holds a string literal value (without quotes).
type StringLit struct {
	Line  int
	Value string
}

func (e *StringLit) node()          {}
func (e *StringLit) exprNode()      {}
func (e *StringLit) String() string { return fmt.Sprintf("%q", e.Value) }

// BoolLit holds a boolean literal (true / false).
type BoolLit struct {
	Line  int
	Value bool
}

func (e *BoolLit) node()          {}
func (e *BoolLit) exprNode()      {}
func (e *BoolLit) String() string { return fmt.Sprintf("%v", e.Value) }

// IdentExpr refers to a variable by name.
type IdentExpr struct {
	Line int
	Name string
}

func (e *IdentExpr) node()          {}
func (e *IdentExpr) exprNode()      {}
func (e *IdentExpr) String() string { return e.Name }

// BinaryExpr: left <op> right
type BinaryExpr struct {
	Line  int
	Op    string
	Left  Expression
	Right Expression
}

func (e *BinaryExpr) node()          {}
func (e *BinaryExpr) exprNode()      {}
func (e *BinaryExpr) String() string {
	return fmt.Sprintf("(%s %s %s)", e.Left, e.Op, e.Right)
}

// UnaryExpr: <op> operand
type UnaryExpr struct {
	Line    int
	Op      string
	Operand Expression
}

func (e *UnaryExpr) node()          {}
func (e *UnaryExpr) exprNode()      {}
func (e *UnaryExpr) String() string { return fmt.Sprintf("(%s%s)", e.Op, e.Operand) }
