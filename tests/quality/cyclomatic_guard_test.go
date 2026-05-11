package quality_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

func TestV700HighRiskFunctionsStayBelowCyclomaticBudget(t *testing.T) {
	targets := []struct {
		file string
		name string
		max  int
	}{
		{file: "internal/workflow/membership_lifecycle.go", name: "MembershipLifecycleWorkflow", max: 14},
		{file: "internal/testsupport/postgres/container.go", name: "StartPool", max: 14},
	}

	for _, target := range targets {
		t.Run(target.name, func(t *testing.T) {
			fn := findFunction(t, target.file, target.name)
			got := cyclomatic(fn)
			if got > target.max {
				t.Fatalf("%s has cyclomatic complexity %d, want <= %d", target.name, got, target.max)
			}
		})
	}
}

func findFunction(t *testing.T, relPath, name string) *ast.FuncDecl {
	t.Helper()
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(root, relPath), nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", relPath, err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == name {
			return fn
		}
	}
	t.Fatalf("function %s not found in %s", name, relPath)
	return nil
}

func cyclomatic(fn *ast.FuncDecl) int {
	if fn.Body == nil {
		return 1
	}
	score := 1
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt:
			score++
		case *ast.CaseClause, *ast.CommClause:
			score++
		case *ast.BinaryExpr:
			score += booleanBranches(node)
		}
		return true
	})
	return score
}

func booleanBranches(expr ast.Expr) int {
	binary, ok := expr.(*ast.BinaryExpr)
	if !ok {
		return 0
	}
	count := booleanBranches(binary.X) + booleanBranches(binary.Y)
	if binary.Op.String() == "&&" || binary.Op.String() == "||" {
		count++
	}
	return count
}
