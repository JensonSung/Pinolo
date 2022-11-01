package stage2

import (
	"errors"
	"github.com/pingcap/tidb/parser/ast"
	"github.com/pingcap/tidb/parser/test_driver"
	_ "github.com/pingcap/tidb/parser/test_driver"
	"reflect"
)
// addFixMWhere0L: FixMWhere0L: *ast.SelectStmt: WHERE xxx -> WHERE 0
func (v *MutateVisitor) addFixMWhere0L(in *ast.SelectStmt, flag int) {
	if in.Where != nil {
		v.addCandidate(FixMWhere0L, 0, in, flag)
	}
}

// doFixMWhere0L: FixMWhere0L: *ast.SelectStmt: WHERE xxx -> WHERE 0
func doFixMWhere0L(rootNode ast.Node, in ast.Node) ([]byte, error) {
	switch in.(type) {
	case *ast.SelectStmt:
		sel := in.(*ast.SelectStmt)
		// check
		if sel.Where == nil {
			return nil, errors.New("FixMWhere0L: sel.Where == nil")
		}
		// mutate
		old := sel.Where

		// WHERE xxx -> WHERE 0
		sel.Where = &test_driver.ValueExpr{
			Datum: test_driver.NewDatum(0),
		}

		sql, err := restore(rootNode)
		if err != nil {
			return nil, errors.New("FixMWhere0L: " +  err.Error())
		}
		// recover
		sel.Where = old
		return sql, nil
	case nil:
		return nil, errors.New("FixMWhere0L: type error: nil")
	default:
		return nil, errors.New("FixMWhere0L: type error: " + reflect.TypeOf(in).String())
	}
}
