package stage2

import (
	"github.com/pkg/errors"
	"github.com/pingcap/tidb/parser/ast"
	"github.com/pingcap/tidb/parser/test_driver"
	_ "github.com/pingcap/tidb/parser/test_driver"
	"math/rand"
	"reflect"
	"strings"
)

// checkRdMRegExpU: return "": pass, otherwise error
func checkRdMRegExpU(in *ast.PatternRegexpExpr) string {
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
		if _, o_k := t.GetValue().(string); !o_k {
			return "in.Pattern: !test_driver.KindString"
		}
	}
	return ""
}

// addRdMRegExpU: RdMRegExpU, *ast.PatternRegexpExpr: '^'|'$' -> '', normal char -> '.', '+'|'?' -> '*'
func (v *MutateVisitor) addRdMRegExpU(in *ast.PatternRegexpExpr, flag int) {
	if checkRdMRegExpU(in) == "" {
		v.addCandidate(RdMRegExpU, 1, in, flag)
	}
}

// doRdMRegExpU: RdMRegExpU, *ast.PatternRegexpExpr: '^'|'$' -> '', normal char -> '.', '+'|'?' -> '*'
func doRdMRegExpU(rootNode ast.Node, in ast.Node, seed int64) ([]byte, error) {
	rander := rand.New(rand.NewSource(seed))
	switch in.(type) {
	case *ast.PatternRegexpExpr:
		re := in.(*ast.PatternRegexpExpr)
		// check
		ck := checkRdMRegExpU(re)
		if ck != "" {
			return nil, errors.New("[doRdMRegExpU]check error " + ck)
		}
		// mutate
		// '^'|'$' -> '', normal char -> '.', '+'|'?' -> '*'
		oldPattern := re.Pattern
		newPattern := []byte(((oldPattern.(*test_driver.ValueExpr)).GetValue()).(string))
		if strings.HasPrefix(string(newPattern), "^") && rander.Intn(2) == 0 {
			newPattern = newPattern[1:]
		}
		if strings.HasSuffix(string(newPattern), "$") && rander.Intn(2) == 0 {
			newPattern = newPattern[:len(newPattern)-1]
		}
		for i, c := range newPattern {
			if (c == '+' || c == '?') && rander.Intn(2) == 0 {
				newPattern[i] = '*'
			}
			// normal char -> '.' is dangerous
		}
		re.Pattern = &test_driver.ValueExpr {
			Datum: test_driver.NewDatum(string(newPattern)),
		}
		sql, err := restore(rootNode)
		if err != nil {
			return nil, errors.Wrap(err, "[doRdMRegExpU]restore error")
		}
		// recover
		re.Pattern = oldPattern
		return sql, nil
	case nil:
		return nil, errors.New("[doRdMRegExpU]type nil")
	default:
		return nil, errors.New("[doRdMRegExpU]type default " + reflect.TypeOf(in).String())
	}
}