package stage2

import (
	"github.com/pkg/errors"
	"github.com/pingcap/tidb/parser/ast"
	"github.com/pingcap/tidb/parser/test_driver"
	_ "github.com/pingcap/tidb/parser/test_driver"
	"math/rand"
	"reflect"
)

// checkRdMLikeL: return "": pass, otherwise error
func checkRdMLikeL(in *ast.PatternLikeExpr) string {
	if t, ok := (in.Expr).(*test_driver.ValueExpr); !ok {
		return "!(in.Expr).(*test_driver.ValueExpr)"
	} else {
		if _, o_k := t.GetValue().(string); !o_k {
			return "in.Expr: !test_driver.KindString"
		}
	}
	if t, ok := (in.Pattern).(*test_driver.ValueExpr); !ok {
		return "!(in.Pattern).(*test_driver.ValueExpr)"
	} else {
		if s, o_k := t.GetValue().(string); !o_k {
			return "in.Pattern: !test_driver.KindString"
		} else {
			o_k = false
			for _, c := range s {
				if c == '%' {
					o_k = true
					break
				}
			}
			if !o_k {
				return "in.Pattern: no '%'"
			}
		}
	}
	return ""
}

// addRdMLikeL: RdMLikeL, *ast.PatternLikeExpr: '%' -> '_'
func (v *MutateVisitor) addRdMLikeL(in *ast.PatternLikeExpr, flag int) {
	if checkRdMLikeL(in) == "" {
		v.addCandidate(RdMLikeL, 0, in, flag)
	}
}

// doRdMLikeL: RdMLikeL, *ast.PatternLikeExpr: '%' -> '_'
func doRdMLikeL(rootNode ast.Node, in ast.Node, seed int64) ([]byte, error) {
	rander := rand.New(rand.NewSource(seed))
	switch in.(type) {
	case *ast.PatternLikeExpr:
		like := in.(*ast.PatternLikeExpr)
		// check
		ck := checkRdMLikeL(like)
		if ck != "" {
			return nil, errors.New("[doRdMLikeL]check error " + ck)
		}
		// mutate
		// '%' -> '_'
		oldPattern := like.Pattern
		newPattern := []byte(((oldPattern.(*test_driver.ValueExpr)).GetValue()).(string))
		for i, c := range newPattern {
			if c == '%' && rander.Intn(2) == 0 {
				newPattern[i] = '_'
			}
		}
		like.Pattern = &test_driver.ValueExpr {
			Datum: test_driver.NewDatum(string(newPattern)),
		}
		sql, err := restore(rootNode)
		if err != nil {
			return nil, errors.Wrap(err, "[doRdMLikeL]restore error")
		}
		// recover
		like.Pattern = oldPattern
		return sql, nil
	case nil:
		return nil, errors.New("[doRdMLikeL]type nil")
	default:
		return nil, errors.New("[doRdMLikeL]type default " + reflect.TypeOf(in).String())
	}
}
