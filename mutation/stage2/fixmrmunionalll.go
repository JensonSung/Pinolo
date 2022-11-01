package stage2

import (
	"errors"
	_ "github.com/pingcap/tidb/parser/test_driver"
	"github.com/pingcap/tidb/parser/ast"
	"reflect"
)

// addFixMRmUnionAllL: FixMRmUnionAllL, *ast.SetOprSelectList: remove Selects[1:] for UNION ALL
func (v *MutateVisitor) addFixMRmUnionAllL(in *ast.SetOprSelectList, flag int) {
	if in.Selects != nil && len(in.Selects) == 2 {
		if sel, ok := in.Selects[1].(*ast.SelectStmt); ok {
			if *sel.AfterSetOperator == ast.UnionAll {
				v.addCandidate(FixMRmUnionAllL, 0, in, flag)
			}
		}
	}
}

// doFixMRmUnionAllL: FixMRmUnionAllL, *ast.SetOprSelectList: remove Selects[1:] for UNION ALL
func doFixMRmUnionAllL(rootNode ast.Node, in ast.Node) ([]byte, error) {
	switch in.(type) {
	case *ast.SetOprSelectList:
		lst := in.(*ast.SetOprSelectList)
		// check
		if lst.Selects == nil || len(lst.Selects) <= 1 {
			return nil, errors.New("doFixMRmUnionAllL: lst.Selects == nil || len(lst.Selects) <= 1")
		}
		// mutate
		oldSels := lst.Selects
		newSels := make([]ast.Node, 0)
		newSels = append(newSels, oldSels[0])
		lst.Selects = newSels
		sql, err := restore(rootNode)
		if err != nil {
			return nil, errors.New("doFixMRmUnionAllL: " +  err.Error())
		}
		// recover
		lst.Selects = oldSels
		return sql, nil
	case nil:
		return nil, errors.New("doFixMRmUnionAllL: type error: nil")
	default:
		return nil, errors.New("doFixMRmUnionAllL: type error: " + reflect.TypeOf(in).String())
	}
}