package stage2

import (
	"errors"
	_ "github.com/pingcap/tidb/parser/test_driver"
	"github.com/pingcap/tidb/parser/ast"
	"reflect"
)

// addFixMDistinctU: FixMDistinctU, *ast.SelectStmt: Distinct true -> false
func (v *MutateVisitor) addFixMDistinctU(in *ast.SelectStmt, flag int) {
	if in.Distinct == true {
		v.addCandidate(FixMDistinctU, 1, in, flag)
	}
}

// doFixMDistinctU: FixMDistinctU, *ast.SelectStmt: Distinct true -> false
func doFixMDistinctU(rootNode ast.Node, in ast.Node) ([]byte, error) {
	switch in.(type) {
	case *ast.SelectStmt:
		sel := in.(*ast.SelectStmt)
		// check
		if sel.Distinct != true {
			return nil, errors.New("doFixMDistinctU: in.Distinct != true")
		}
		// mutate
		sel.Distinct = false
		sql, err := restore(rootNode)
		if err != nil {
			return nil, errors.New("doFixMDistinctU: " +  err.Error())
		}
		// recover
		sel.Distinct = true
		return sql, nil
	case nil:
		return nil, errors.New("doFixMDistinctU: type error: nil")
	default:
		return nil, errors.New("doFixMDistinctU: type error: " + reflect.TypeOf(in).String())
	}
}

// addFixMDistinctL: FixMDistinctL: *ast.SelectStmt: Distinct false -> true
func (v *MutateVisitor) addFixMDistinctL(in *ast.SelectStmt, flag int) {
	if in.Distinct == false {
		v.addCandidate(FixMDistinctL, 0, in, flag)
	}
}

// doFixMDistinctL: FixMDistinctL: *ast.SelectStmt: Distinct false -> true
func doFixMDistinctL(rootNode ast.Node, in ast.Node) ([]byte, error) {
	switch in.(type) {
	case *ast.SelectStmt:
		sel := in.(*ast.SelectStmt)
		// check
		if sel.Distinct != false {
			return nil, errors.New("doFixMDistinctL: in.Distinct != false")
		}
		// mutate
		sel.Distinct = true
		sql, err := restore(rootNode)
		if err != nil {
			return nil, errors.New("doFixMDistinctL: " +  err.Error())
		}
		// recover
		sel.Distinct = false
		return sql, nil
	case nil:
		return nil, errors.New("doFixMDistinctU: type error: nil")
	default:
		return nil, errors.New("doFixMDistinctL: type error: " + reflect.TypeOf(in).String())
	}
}