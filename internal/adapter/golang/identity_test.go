package golang

import (
	"go/types"
	"testing"
)

func TestDeclaredFuncIdentityMakesInitUnique(t *testing.T) {
	pkg := types.NewPackage("example.com/project", "project")
	fn := types.NewFunc(0, pkg, "init", types.NewSignatureType(nil, nil, nil, nil, nil, false))

	keyA, qnameA, _, _ := declaredFuncIdentity(fn, "a.go", 10, 2)
	keyB, qnameB, _, _ := declaredFuncIdentity(fn, "b.go", 10, 2)

	if keyA == keyB {
		t.Fatalf("expected init keys to differ, got %q", keyA)
	}
	if qnameA == qnameB {
		t.Fatalf("expected init qnames to differ, got %q", qnameA)
	}
}

func TestDeclaredFuncIdentityKeepsRegularFunctionStable(t *testing.T) {
	pkg := types.NewPackage("example.com/project", "project")
	fn := types.NewFunc(0, pkg, "Run", types.NewSignatureType(nil, nil, nil, nil, nil, false))

	keyA, qnameA, _, _ := declaredFuncIdentity(fn, "a.go", 10, 2)
	keyB, qnameB, _, _ := declaredFuncIdentity(fn, "b.go", 99, 1)

	if keyA != keyB {
		t.Fatalf("expected regular function key to stay stable, got %q and %q", keyA, keyB)
	}
	if qnameA != qnameB {
		t.Fatalf("expected regular function qname to stay stable, got %q and %q", qnameA, qnameB)
	}
}
