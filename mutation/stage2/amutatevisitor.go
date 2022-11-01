package stage2

import (
	"bytes"
	"errors"
	"github.com/pingcap/tidb/parser/ast"
	"github.com/pingcap/tidb/parser/format"
	"github.com/pingcap/tidb/parser/opcode"
	_ "github.com/pingcap/tidb/parser/test_driver"
	"log"
	"strings"
)

// Candidate: (mutation name, U, candidate node, Flag).
//
// Flag: 1: positive, 0: negative.
//
// U: when positive, 1: upper mutation, 0: lower mutation.
//
// example:
//   [positive]
//     SELECT * FROM T WHERE X > 0;
//     [ upper mutation ] X > 0 -> X >= 0
//     The result set will expand
//   [negative]
//     SELECT * FROM T WHERE (X > 0) IS FALSE;
//     [upper mutation ] x > 0 -> X >= 0
//     The result set will shrink
//   [negative]
//     SELECT * FROM T WHERE (X > 0) IS FALSE;
//     [lower mutation ] x > 0 -> X > 1
//     The result set will expand
// Obviously you should use ((U^Flag)^1) == 1 to calculate the effect of mutation
type Candidate struct {
	MutationName string // mutation name
	// 1: upper mutation, strings.HasSuffix(MutationName, "U"): true;
	// 0: lower mutation, strings.HasSuffix(MutationName, "L"): true
	U    int
	Node ast.Node // candidate node
	Flag int      // 1: positive, 0: negative
}

// MutateVisitor: visit the sub-AST and obtain the candidate set of
// mutation points, see Candidates.
//
// Each mutation has its own name, see:
//   FixMDistinctU
//	 FixMDistinctL
//	 FixMCmpOpU
//	 FixMCmpOpL
//	 FixMUnionAllU
//	 FixMUnionAllL
//   FixMInNullU
//	 FixMWhere1U
//	 FixMWhere0L
//	 FixMHaving1U
//	 FixMHaving0L
//	 FixMOn1U
//	 FixMOn0L
//	 FixMRmUnionAllL
//	 RdMLikeU
//	 RdMLikeL
//	 RdMRegExpU
//	 RdMRegExpL
//
// about the prefix {FixM|RdM}(currently not working):
//   FixM means fixed mutation;
//   RdM means random mutation;
// about the suffix {U|L}:
//   U means upper mutation,
//   L means lower mutation.
// There are lots of functions under MutateVisitor, we classify them by prefix:
//   visitxxx: calculate flag, call miningxxx
//   miningxxx: call addxxx
//   addxxx: add mutation u/l to Candidates
type MutateVisitor struct {
	Root ast.Node
	Candidates map[string][]*Candidate // mutation name : slice of *Candidate
}

func (v *MutateVisitor) visit(in ast.Node, flag int) {
	switch in.(type) {
	case *ast.SetOprStmt:
		v.visitSetOprStmt(in.(*ast.SetOprStmt), flag)
	case *ast.SelectStmt:
		v.visitSelect(in.(*ast.SelectStmt), flag)
	}
}

func (v *MutateVisitor) visitSetOprStmt(in *ast.SetOprStmt, flag int) {
	if in == nil {
		return
	}
	v.visitWithClause(in.SelectList.With, flag)
	v.visitSetOprSelectList(in.SelectList, flag)
}

// visitSetOprSelectList: miningSetOprSelectList
func (v *MutateVisitor) visitSetOprSelectList(in *ast.SetOprSelectList, flag int) {
	// Note that MySQL only has UNION [ALL]
	if in == nil {
		return
	}
	v.visitWithClause(in.With, flag)
	for _, sel := range in.Selects {
		switch sel.(type) {
		case *ast.SetOprSelectList:
			v.visitSetOprSelectList(sel.(*ast.SetOprSelectList), flag)
		case *ast.SelectStmt:
			v.visitSelect(sel.(*ast.SelectStmt), flag)
		}
	}

	v.miningSetOprSelectList(in, flag)
}

func (v *MutateVisitor) visitWithClause(in *ast.WithClause, flag int) {
	if in == nil {
		return
	}
	// cannot support recursive WITH
	if in.IsRecursive {
		return
	}
	for _, cte := range in.CTEs {
		v.visitSubqueryExpr(cte.Query, flag)
	}
}

func (v *MutateVisitor) visitSubqueryExpr(in *ast.SubqueryExpr, flag int) {
	if in == nil {
		return
	}
	v.visitResultSetNode(in.Query, flag)
}

// visitResultSetNode: important bridge, include
// SelectStmt, SubqueryExpr, TableSource, TableName, Join and SetOprStmt.
func (v *MutateVisitor) visitResultSetNode(in ast.ResultSetNode, flag int) {
	if in == nil {
		return
	}
	switch in.(type) {
	case *ast.SelectStmt:
		v.visitSelect(in.(*ast.SelectStmt), flag)
	case *ast.SubqueryExpr:
		v.visitSubqueryExpr(in.(*ast.SubqueryExpr), flag)
	case *ast.TableSource:
		v.visitTableSource(in.(*ast.TableSource), flag)
	case *ast.TableName:
		// skip
	case *ast.Join:
		v.visitJoin(in.(*ast.Join), flag)
	case *ast.SetOprStmt:
		v.visitSetOprStmt(in.(*ast.SetOprStmt), flag)
	}
}

func (v *MutateVisitor) visitTableSource(in *ast.TableSource, flag int) {
	if in == nil {
		return
	}
	v.visitResultSetNode(in.Source, flag)
}

// visitSelect: miningSelectStmt
func (v *MutateVisitor) visitSelect(in *ast.SelectStmt, flag int) {
	if in == nil {
		return
	}

	// from
	v.visitTableRefClause(in.From, flag)
	// where
	v.visitExprNode(in.Where, flag)
	// having
	v.visitHavingClause(in.Having, flag)
	// with
	v.visitWithClause(in.With, flag)

	v.miningSelectStmt(in, flag)
}

func (v *MutateVisitor) visitTableRefClause(in *ast.TableRefsClause, flag int) {
	if in == nil {
		return
	}
	v.visitJoin(in.TableRefs, flag)
}

func (v *MutateVisitor) visitJoin(in *ast.Join, flag int) {
	if in == nil {
		return
	}
	// skip left | right join
	if in.Tp == ast.LeftJoin || in.Tp == ast.RightJoin {
		return
	}
	v.visitResultSetNode(in.Left, flag)
	v.visitResultSetNode(in.Right, flag)
	// on
	v.visitOnCondition(in.On, flag)

	v.miningJoin(in, flag)
}

func (v *MutateVisitor) visitOnCondition(in *ast.OnCondition, flag int) {
	if in == nil {
		return
	}
	v.visitExprNode(in.Expr, flag)
}

func (v *MutateVisitor) visitHavingClause(in *ast.HavingClause, flag int) {
	if in == nil {
		return
	}
	v.visitExprNode(in.Expr, flag)
}

// visitExprNode: important bridge
func (v *MutateVisitor) visitExprNode(in ast.ExprNode, flag int) {
	if in == nil {
		return
	}
	switch in.(type) {
	//case ast.FuncNode:
	//case ast.ValueExpr:
	case *ast.BetweenExpr:
		// type conversion, discard!
	case *ast.BinaryOperationExpr:
		v.visitBinaryOperationExpr(in.(*ast.BinaryOperationExpr), flag)
	case *ast.CaseExpr:
		// skip
	case *ast.SubqueryExpr:
		v.visitSubqueryExpr(in.(*ast.SubqueryExpr), flag)
	case *ast.CompareSubqueryExpr:
		v.visitCompareSubqueryExpr(in.(*ast.CompareSubqueryExpr), flag)
	case *ast.TableNameExpr:
		// skip
	case *ast.ColumnNameExpr:
		// skip
	case *ast.DefaultExpr:
		// skip
	case *ast.ExistsSubqueryExpr:
		v.visitExistsSubqueryExpr(in.(*ast.ExistsSubqueryExpr), flag)
	case *ast.PatternInExpr:
		v.visitPatternInExpr(in.(*ast.PatternInExpr), flag)
	case *ast.IsNullExpr:
		v.visitIsNullExpr(in.(*ast.IsNullExpr), flag)
	case *ast.IsTruthExpr:
		v.visitIsTruthExpr(in.(*ast.IsTruthExpr), flag)
	case *ast.PatternLikeExpr:
		v.visitPatternLikeExpr(in.(*ast.PatternLikeExpr), flag)
	//case ast.ParamMarkerExpr:
	case *ast.ParenthesesExpr:
		v.visitParenthesesExpr(in.(*ast.ParenthesesExpr), flag)
	case *ast.PositionExpr:
		// skip
	case *ast.PatternRegexpExpr:
		v.visitPatternRegexpExpr(in.(*ast.PatternRegexpExpr), flag)
	case *ast.RowExpr:
		// skip
	case *ast.UnaryOperationExpr:
		v.visitUnaryOperationExpr(in.(*ast.UnaryOperationExpr), flag)
	case *ast.ValuesExpr:
		// skip
	case *ast.VariableExpr:
		// skip
	case *ast.MaxValueExpr:
		// skip
		// https://dev.mysql.com/doc/refman/8.0/en/partitioning-range.html
	case *ast.MatchAgainst:
		// skip, todo
	case *ast.SetCollationExpr:
		// skip
	case *ast.FuncCallExpr:
		v.visitFuncCallExpr(in.(*ast.FuncCallExpr), flag)
	case *ast.FuncCastExpr:
		v.visitFuncCastExpr(in.(*ast.FuncCastExpr), flag)
	case *ast.TrimDirectionExpr:
		v.visitTrimDirectionExpr(in.(*ast.TrimDirectionExpr), flag)
	case *ast.AggregateFuncExpr:
		// skip
	case *ast.WindowFuncExpr:
		// skip
	case *ast.TimeUnitExpr:
		// skip
	case *ast.GetFormatSelectorExpr:
		// skip
	}
}

// visitBinaryOperationExpr: important bridge, miningBinaryOperationExpr
func (v *MutateVisitor) visitBinaryOperationExpr(in *ast.BinaryOperationExpr, flag int) {
	if in == nil {
		return
	}
	switch in.Op {
	case opcode.LogicAnd:
		v.visitExprNode(in.L, flag)
		v.visitExprNode(in.R, flag)
	case opcode.LeftShift:
		// numeric skip
	case opcode.RightShift:
		// numeric skip
	case opcode.LogicOr:
		v.visitExprNode(in.L, flag)
		v.visitExprNode(in.R, flag)
	case opcode.GE:
		// cmp mutation, see miningBinaryOperationExpr
	case opcode.LE:
		// cmp mutation, see miningBinaryOperationExpr
	case opcode.EQ:
		// cmp mutation, see miningBinaryOperationExpr
	case opcode.NE:
		// cmp mutation, see miningBinaryOperationExpr
	case opcode.LT:
		// cmp mutation, see miningBinaryOperationExpr
	case opcode.GT:
		// cmp mutation, see miningBinaryOperationExpr
	case opcode.Plus:
		// numeric skip
	case opcode.Minus:
		// numeric skip
	case opcode.And:
		// numeric skip
	case opcode.Or:
		// numeric skip
	case opcode.Mod:
		// numeric skip
	case opcode.Xor:
		// numeric skip
	case opcode.Div:
		// numeric skip
	case opcode.Mul:
		// numeric skip
	//case opcode.Not:
	//case opcode.Not2:
	//case opcode.BitNeg:
	case opcode.IntDiv:
		// numeric skip
	case opcode.LogicXor:
		// skip
	case opcode.NullEQ:
		// skip
		//case opcode.In:
		//case opcode.Like:
		//case opcode.Case:
		//case opcode.Regexp:
		//case opcode.IsNull:
		//case opcode.IsTruth:
		//case opcode.IsFalsity:
	}

	v.miningBinaryOperationExpr(in, flag)
}

// visitCompareSubqueryExpr: miningCompareSubqueryExpr
func (v *MutateVisitor) visitCompareSubqueryExpr(in *ast.CompareSubqueryExpr, flag int) {
	if in == nil {
		return
	}
	// before All
	v.miningCompareSubqueryExpr(in, flag)
	// in.all false: ANY, in.all true: ALL
	if in.All {
		flag = flag ^ 1
	}
	switch (in.R).(type) {
	case *ast.SubqueryExpr:
		v.visitSubqueryExpr((in.R).(*ast.SubqueryExpr), flag)
	}
}

func (v *MutateVisitor) visitExistsSubqueryExpr(in *ast.ExistsSubqueryExpr, flag int) {
	if in == nil {
		return
	}
	if in.Not {
		flag = flag ^ 1
	}
	switch (in.Sel).(type) {
	case *ast.SubqueryExpr:
		v.visitSubqueryExpr((in.Sel).(*ast.SubqueryExpr), flag)
	}
}

// visitPatternInExpr: miningPatternInExpr
func (v *MutateVisitor) visitPatternInExpr(in *ast.PatternInExpr, flag int) {
	if in == nil {
		return
	}
	if in.Not {
		flag = flag ^ 1
	}
	// IN (XXX,XXX,XXX) OR IN (SUBQUERY)?
	switch (in.Sel).(type) {
	case *ast.SubqueryExpr:
		v.visitSubqueryExpr((in.Sel).(*ast.SubqueryExpr), flag)
	default:
		// after in.Not
		v.miningPatternInExpr(in, flag)
	}
}

func (v *MutateVisitor) visitIsNullExpr(in *ast.IsNullExpr, flag int) {
	if in == nil {
		return
	}
	// skip
}

func (v *MutateVisitor) visitIsTruthExpr(in *ast.IsTruthExpr, flag int) {
	if in == nil {
		return
	}
	if in.Not {
		// IS NOT
		flag = flag ^ 1
	}
	if in.True <= 0 {
		// FALSE
		flag = flag ^ 1
	}
	v.visitExprNode(in.Expr, flag)
}

// visitPatternLikeExpr: miningPatternLikeExpr
func (v *MutateVisitor) visitPatternLikeExpr(in *ast.PatternLikeExpr, flag int) {
	if in == nil {
		return
	}
	if in.Not {
		flag = flag ^ 1
	}
	// after in.Not
	v.miningPatternLikeExpr(in, flag)
}

// visitParenthesesExpr: ()
func (v *MutateVisitor) visitParenthesesExpr(in *ast.ParenthesesExpr, flag int) {
	if in == nil {
		return
	}
	v.visitExprNode(in.Expr, flag)
}

// visitPatternRegexpExpr: miningPatternRegexpExpr
func (v *MutateVisitor) visitPatternRegexpExpr(in *ast.PatternRegexpExpr, flag int) {
	if in == nil {
		return
	}
	if in.Not {
		flag = flag ^ 1
	}
	// after in.Not
	v.miningPatternRegexpExpr(in, flag)
}

// visitUnaryOperationExpr: important bridge
func (v *MutateVisitor) visitUnaryOperationExpr(in *ast.UnaryOperationExpr, flag int) {
	if in == nil {
		return
	}
	switch in.Op {
	case opcode.Plus:
		// numeric skip
	case opcode.Minus:
		// numeric skip
	case opcode.Not:
		flag = flag ^ 1
		v.visitExprNode(in.V, flag)
	case opcode.Not2:
		flag = flag ^ 1
		v.visitExprNode(in.V, flag)
	case opcode.BitNeg:
		// numeric skip
	}
}

func (v *MutateVisitor) visitFuncCallExpr(in *ast.FuncCallExpr, flag int) {
	if in == nil {
		return
	}
	// skip func call
}

func (v *MutateVisitor) visitFuncCastExpr(in *ast.FuncCastExpr, flag int) {
	if in == nil {
		return
	}
	// skip cast
}

func (v *MutateVisitor) visitTrimDirectionExpr(in *ast.TrimDirectionExpr, flag int) {
	if in == nil {
		return
	}
	// skip trim
}

func (v *MutateVisitor) miningSetOprSelectList(in *ast.SetOprSelectList, flag int) {
	// FixMRmUnionAllL
	v.addFixMRmUnionAllL(in, flag)
}

func (v *MutateVisitor) miningSelectStmt(in *ast.SelectStmt, flag int) {
	// FixMDistinctU
	v.addFixMDistinctU(in, flag)
	// FixMDistinctL
	v.addFixMDistinctL(in, flag)
	// FixMUnionAllU
	v.addFixMUnionAllU(in, flag)
	// FixMUnionAllL
	v.addFixMUnionAllL(in, flag)
	// FixMWhere1U
	v.addFixMWhere1U(in, flag)
	// FixMWhere0L
	v.addFixMWhere0L(in, flag)
	// FixMHaving1U
	v.addFixMHaving1U(in, flag)
	// FixMHaving0L
	v.addFixMHaving0L(in, flag)
}

func (v *MutateVisitor) miningJoin(in *ast.Join, flag int) {
	// FixMOn1U
	v.addFixMOn1U(in, flag)
	// FixMOn0L
	v.addFixMOn0L(in, flag)
}

func (v *MutateVisitor) miningBinaryOperationExpr(in *ast.BinaryOperationExpr, flag int) {
	// FixMCmpOpU
	v.addFixMCmpOpU(in, flag)
	// FixMCmpOpL
	v.addFixMCmpOpL(in, flag)
}

func (v *MutateVisitor) miningCompareSubqueryExpr(in *ast.CompareSubqueryExpr, flag int) {
	// FixMCmpOpU
	v.addFixMCmpOpU(in, flag)
	// FixMCmpOpL
	v.addFixMCmpOpL(in, flag)
}

func (v *MutateVisitor) miningPatternInExpr(in *ast.PatternInExpr, flag int) {
	// FixMInNullU
	v.addFixMInNullU(in, flag)
}

func (v *MutateVisitor) miningPatternLikeExpr(in *ast.PatternLikeExpr, flag int) {
	// RdMLikeU
	v.addRdMLikeU(in, flag)
	// RdMLikeL
	v.addRdMLikeL(in, flag)
}

func (v *MutateVisitor) miningPatternRegexpExpr(in *ast.PatternRegexpExpr, flag int) {
	// RdMRegExpU
	v.addRdMRegExpU(in, flag)
	// RdMRegExpL
	v.addRdMRegExpL(in, flag)
}

func (v *MutateVisitor) addCandidate(mutationName string, u int, in ast.Node, flag int) {
	if strings.HasSuffix(mutationName, "U") && u == 0 {
		log.Fatal("strings.HasSuffix(mutationName, \"U\") && u == 0")
	}
	if strings.HasSuffix(mutationName, "L") && u != 0 {
		log.Fatal("strings.HasSuffix(mutationName, \"L\") && u != 0")
	}
	var ls []*Candidate = nil
	ok := false
	if ls, ok = v.Candidates[mutationName]; !ok {
		ls = make([]*Candidate, 0)
	}
	ls = append(ls, &Candidate{
		MutationName: mutationName,
		U:            u,
		Node:         in,
		Flag:         flag,
	})
	v.Candidates[mutationName] = ls
}

func restore(rootNode ast.Node) ([]byte, error) {
	buf := new(bytes.Buffer)
	ctx := format.NewRestoreCtx(format.DefaultRestoreFlags, buf)
	err := rootNode.Restore(ctx)
	if err != nil {
		return nil, errors.New("restore error: " + err.Error())
	}
	return buf.Bytes(), nil
}