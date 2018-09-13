package main

// Parser parses tokens into AbstractSyntaxTree.
type Parser struct {
	tokenizer *Tokenizer

	// how many breakable state the parser is in.
	// a positive breakCount means we can break breakCount times.
	breakCount uint

	// if we want to parse a statement without eating out the end semicolon,
	// set it to true. And it will be cleared immediately after a statement is parsed.
	skipSemicolon bool
}

// NewParser news a parser.
func NewParser(tokenizer *Tokenizer) *Parser {
	return &Parser{
		tokenizer:     tokenizer,
		breakCount:    0,
		skipSemicolon: false,
	}
}

// Parse does parse the input tokens.
func (p *Parser) Parse() (program *Program, err error) {
	defer func() {
		err = toErr(recover())
	}()

	program = &Program{}
	for {
		if p.follow(ttEOF) {
			break
		}
		stmt := p.parseStatement(true)
		program.stmts = append(program.stmts, stmt)
	}
	tk := p.next()
	if tk.typ != ttEOF {
		panicf("unexpected token: %v", tk)
	}

	return program, nil
}

func (p *Parser) expect(tt TokenType) Token {
	token := p.next()
	if token.typ != tt {
		exp := Token{typ: tt}.String()
		switch tt {
		case ttIdentifier:
			exp = "`identifier'"
		}
		panicf("unexpected token: %v (expect: %v)", token, exp)
	}
	return token
}

func (p *Parser) match(tts ...TokenType) (Token, bool) {
	tk := p.next()
	for _, tt := range tts {
		if tk.typ == tt {
			return tk, true
		}
	}
	p.undo(tk)
	return Token{}, false
}

func (p *Parser) isOp(t Token) bool {
	return t.typ >= ttQuestion && t.typ <= ttDecrement
}

func (p *Parser) next() Token {
	return p.tokenizer.Next()
}

func (p *Parser) undo(tk Token) {
	p.tokenizer.Undo(tk)
}

func (p *Parser) skip(tt TokenType) bool {
	if p.follow(tt) {
		p.next()
		return true
	}
	return false
}

func (p *Parser) peek() Token {
	return p.tokenizer.Peek()
}

func (p *Parser) follow(tt TokenType) bool {
	return p.peek().typ == tt
}

func (p *Parser) enter() {
	p.tokenizer.PushFrame()
}

func (p *Parser) leave(putback bool) {
	p.tokenizer.PopFrame(putback)
}

func (p *Parser) parseStatement(global bool) Statement {
	tk := p.peek()

	switch tk.typ {
	case ttLet:
		return p.parseVariableStatement()
	case ttFunction:
		// // don't know whether it is a function statement or function expression.
		// // but, if a function doesn't have a name, it must be function expression.
		// p.enter()
		fn := p.parseFunctionStatement()
		// if fn.expr.name == "" {
		// 	p.leave(true)
		// 	// TODO we should directly parse function expression statement since we knew it is.
		// 	stmt := p.parseExpressionStatement()
		// 	if stmt == nil {
		//
		// 		panic("anonymous function expression must be called immediately")
		// 	}
		// }
		// p.leave(false)
		return fn
	case ttSemicolon:
		p.next()
		return &EmptyStatement{}
	}

	if global {
		panic("non-global statement")
	}

	switch tk.typ {
	case ttReturn:
		return p.parseReturnStatement()
	case ttLeftBrace:
		// Notice: block statement skips parsing {} as object literal.
		return p.parseBlockStatement()
	case ttFor:
		return p.parseForStatement()
	case ttBreak:
		if p.breakCount <= 0 {
			panic("break statement must be in for-loop or switch")
		}
		return p.parseBreakStatement()
	case ttIf:
		return p.parseIfStatement()
	case ttSwitch:
		return p.parseSwitchStatement()
	}

	if stmt := p.tryParseExpressionStatement(); stmt != nil {
		return stmt
	}
	if stmt := p.tryParseAssignmentStatement(); stmt != nil {
		return stmt
	}

	panicf("unknown statement at line: %d", tk.line)
	return nil
}

func (p *Parser) parseVariableStatement() *VariableStatement {
	var v VariableStatement
	p.expect(ttLet)
	v.Name = p.expect(ttIdentifier).str
	if p.follow(ttAssign) {
		p.next()
		v.Expr = p.parseExpression(ttQuestion)
	}
	p.expect(ttSemicolon)
	return &v
}

func (p *Parser) tryParseAssignmentStatement() (stmt *AssignmentStatement) {
	p.enter()
	defer func() {
		recover()
		p.leave(stmt == nil)
	}()

	var as AssignmentStatement

	as.left = p.parseExpression(ttQuestion)

	if _, ok := p.match(ttAssign); ok {
		as.right = p.parseExpression(ttQuestion)
	} else if op, ok := p.match(
		ttStarStarAssign,
		ttStarAssign, ttDivideAssign, ttPercentAssign,
		ttPlusAssign, ttMinusAssign,
		ttLeftShiftAssign, ttRightShiftAssign,
		ttAndAssign, ttOrAssign, ttXorAssign, ttAndNotAssign,
	); ok {
		right := p.parseExpression(ttQuestion)
		var binOp TokenType
		switch op.typ {
		case ttStarStarAssign:
			binOp = ttStarStar
		case ttStarAssign:
			binOp = ttMultiply
		case ttDivideAssign:
			binOp = ttDivision
		case ttPercentAssign:
			binOp = ttPercent
		case ttPlusAssign:
			binOp = ttAddition
		case ttMinusAssign:
			binOp = ttSubstraction
		case ttLeftShiftAssign:
			binOp = ttLeftShift
		case ttRightShiftAssign:
			binOp = ttRightShift
		case ttAndAssign:
			binOp = ttBitAnd
		case ttOrAssign:
			binOp = ttBitOr
		case ttXorAssign:
			binOp = ttBitXor
		case ttAndNotAssign:
			binOp = ttBitAndNot
		default:
			panic("won't go here")
		}
		as.right = NewBinaryExpression(as.left, binOp, right)
	} else {
		return nil
	}

	if !p.skipSemicolon {
		// hah? !skip? skip?
		p.skip(ttSemicolon)
	} else {
		p.skipSemicolon = false
	}

	return &as
}

func (p *Parser) parseFunctionStatement() *FunctionStatement {
	var fn FunctionStatement
	p.expect(ttFunction)
	expr := p.parseFunctionExpression()
	fn.expr = expr
	return &fn
}

func (p *Parser) parseReturnStatement() *ReturnStatement {
	p.expect(ttReturn)
	ret := &ReturnStatement{}
	if !p.follow(ttSemicolon) {
		ret.expr = p.parseExpression(ttQuestion)
	}
	p.expect(ttSemicolon)
	return ret
}

func (p *Parser) tryParseExpressionStatement() (stmt *ExpressionStatement) {
	p.enter()
	defer func() {
		recover()
		p.leave(stmt == nil)
	}()

	expr := p.parseExpression(ttQuestion)

	stmt = &ExpressionStatement{
		expr: expr,
	}
	if p.skip(ttSemicolon) {
		return stmt
	}
	return nil
}

func (p *Parser) parseBlockStatement() (stmt *BlockStatement) {
	block := &BlockStatement{}
	p.expect(ttLeftBrace)
	for {
		if p.follow(ttRightBrace) {
			break
		}
		stmt := p.parseStatement(false)
		block.stmts = append(block.stmts, stmt)
	}
	p.expect(ttRightBrace)

	return block
}

// All three parts of for-stmt (init, test, incr) can be omitted.
// If all these parts are omitted, the two semicolons can be omitted.
//   for [init]; [test]; [incr] {}
//   for expr {}
//   for {}
func (p *Parser) parseForStatement() *ForStatement {
	p.expect(ttFor)

	var fs ForStatement

	hasInit := false

	if p.follow(ttLet) {
		hasInit = true
		// TODO init can be assignment
		fs.init = p.parseVariableStatement()
	} else if p.follow(ttSemicolon) {
		hasInit = true
		p.expect(ttSemicolon)
	} else if !p.follow(ttLeftBrace) {
		fs.test = p.parseExpression(ttQuestion)
		hasInit = false
	}

	if hasInit {
		// test
		if !p.follow(ttSemicolon) {
			fs.test = p.parseExpression(ttQuestion)
			p.expect(ttSemicolon)
		} else {
			p.next()
			// no test
		}
		// incr
		if !p.follow(ttLeftBrace) {
			// is expr?
			p.enter()
			fs.incr = p.parseExpression(ttQuestion)
			if !p.follow(ttLeftBrace) {
				p.leave(true)
				fs.incr = nil
			} else {
				p.leave(false)
			}
			// is assignment?
			if fs.incr == nil {
				p.enter()
				p.skipSemicolon = true
				fs.incr = p.tryParseAssignmentStatement()
				if !p.follow(ttLeftBrace) {
					p.leave(true)
					fs.incr = nil
				} else {
					p.leave(false)
				}
			}
			if fs.incr == nil {
				panic("incr expected")
			}
		} else {
			// no incr
		}
	} else {
		if !p.follow(ttLeftBrace) {
			panic("for needs body")
		}
	}

	p.breakCount++

	fs.block = p.parseBlockStatement()
	if fs.block == nil {
		panic("for needs body")
	}

	p.breakCount--

	return &fs
}

func (p *Parser) parseBreakStatement() *BreakStatement {
	p.expect(ttBreak)
	p.expect(ttSemicolon)
	return &BreakStatement{}
}

func (p *Parser) parseIfStatement() *IfStatement {
	p.expect(ttIf)
	expr := p.parseExpression(ttQuestion)
	ifBlock := p.parseBlockStatement()
	var elseBlock Statement
	switch p.peek().typ {
	case ttElse:
		p.expect(ttElse)
		switch p.peek().typ {
		case ttIf:
			elseBlock = p.parseIfStatement()
		case ttLeftBrace:
			elseBlock = p.parseBlockStatement()
		default:
			panic("else expect if or block to follow")
		}
	}
	return &IfStatement{
		cond:      expr,
		ifBlock:   ifBlock,
		elseBlock: elseBlock,
	}
}

func (p *Parser) parseSwitchStatement() *SwitchStatement {
	p.expect(ttSwitch)
	ss := SwitchStatement{}
	ss.cond = p.parseExpression(ttQuestion)
	p.expect(ttLeftBrace)
	// empty cases
	if p.follow(ttRightBrace) {
		p.next()
		return &ss
	}
	// parse cases
	for {
		var group *CaseGroup
		switch esac := p.next(); esac.typ {
		case ttDefault:
			if ss.def != nil {
				panic("duplicate default")
			}
			p.expect(ttColon)
			group = &CaseGroup{}
			ss.def = group
		case ttCase:
			group = &CaseGroup{}
			ss.cases = append(ss.cases, group)
			for {
				expr := p.parseExpression(ttQuestion)
				group.cases = append(group.cases, expr)
				p.skip(ttComma)
				if p.follow(ttColon) {
					p.next()
					break
				}
			}
		default:
			panicf("unexpected token: %v", esac)
		}
		// no need to be real block.
		// but we construct a block since statements are executed in scope.
		group.block = &BlockStatement{}
		// we can break in switch statement
		p.breakCount++
		for {
			if tk, ok := p.match(ttCase, ttDefault, ttRightBrace); ok {
				p.undo(tk)
				break
			}
			stmt := p.parseStatement(false)
			group.block.stmts = append(group.block.stmts, stmt)
		}
		p.breakCount--
		if p.follow(ttRightBrace) {
			p.next()
			break
		}
	}
	return &ss
}

func (p *Parser) parseExpression(level TokenType) Expression {
	var left Expression
	switch next := p.peek(); next.typ {
	case ttNot, ttBitXor, ttAddition, ttSubstraction:
		p.next()
		right := p.parseExpression(ttIncrement)
		left = NewUnaryExpression(next.typ, right)
	case ttIncrement, ttDecrement:
		p.next()
		right := p.parseExpression(ttIncrement)
		left = NewIncrementDecrementExpression(next.typ, true, right)
	default:
		left = p.parsePrimaryExpression()
	}

	for {
		op := p.next()
		if !p.isOp(op) || op.typ < level {
			p.undo(op)
			break
		}

		switch op.typ {
		case ttQuestion:
			left = p.parseTernaryExpression(left)
			continue
		case ttIncrement, ttDecrement:
			left = NewIncrementDecrementExpression(op.typ, false, left)
			continue
		}

		var right Expression

		switch op.typ {
		case ttAndAnd, ttOrOr:
			right = p.parseExpression(ttBitAnd)
		case ttBitAnd, ttBitOr, ttBitXor, ttBitAndNot:
			right = p.parseExpression(ttEqual)
		case ttEqual, ttNotEqual:
			right = p.parseExpression(ttGreaterThan)
		case ttGreaterThan, ttGreaterThanOrEqual, ttLessThan, ttLessThanOrEqual:
			right = p.parseExpression(ttLeftShift)
		case ttLeftShift, ttRightShift:
			right = p.parseExpression(ttAddition)
		case ttAddition, ttSubstraction:
			right = p.parseExpression(ttMultiply)
		case ttMultiply, ttDivision, ttPercent:
			right = p.parseExpression(ttStarStar)
		case ttStarStar:
			right = p.parseExpression(ttStarStar)
		default:
			panicf("unhandled operator: %v", op)
		}

		left = NewBinaryExpression(left, op.typ, right)
	}

	return left
}

func (p *Parser) parseTernaryExpression(cond Expression) Expression {
	var left, right Expression
	// Although we don't allow nested ternary expression, we parse it, we panic it, later.
	// left = p.parseLogicalExpression()
	left = p.parseExpression(ttQuestion)
	p.expect(ttColon)
	right = p.parseExpression(ttQuestion)
	if _, ok := left.(*TernaryExpression); ok {
		panic("nested ?: is not allowed")
	}
	if _, ok := right.(*TernaryExpression); ok {
		panic("nested ?: is not allowed")
	}
	return NewTernaryExpression(cond, left, right)
}

func (p *Parser) parsePrimaryExpression() Expression {
	var expr Expression

	next := p.next()

	switch next.typ {
	case ttNil:
		expr = ValueFromNil()
	case ttBoolean:
		expr = ValueFromBoolean(next.str == "true")
	case ttNumber:
		expr = ValueFromNumber(next.num)
	case ttString:
		expr = ValueFromString(next.str)
	case ttLeftParen:
		p.undo(next)
		if lambda := p.tryParseLambdaExpression(false); lambda != nil {
			return lambda
		}
		p.next()
		expr = p.parseExpression(ttQuestion)
		p.expect(ttRightParen)
	case ttIdentifier:
		if p.follow(ttLambda) {
			p.undo(next)
			return p.tryParseLambdaExpression(true)
		}
		expr = ValueFromVariable(next.str)
	case ttFunction:
		expr = p.parseFunctionExpression()
	case ttLeftBrace:
		p.undo(next)
		expr = p.parseObjectExpression()
	case ttLeftBracket:
		p.undo(next)
		expr = p.parseArrayExpression()
	default:
		p.undo(next)
		expr = nil
	}

	if expr == nil {
		panicf("unknown expression at line: %d", next.line)
	}

	for {
		if index := p.tryParseIndexExpression(expr); index != nil {
			expr = index
			continue
		}
		if call := p.tryParseCallExpression(expr); call != nil {
			expr = call
			continue
		}
		break
	}

	return expr
}

// tryParseLambdaExpression tries to parse a lambda expression.
// If must, and no lambda can be parsed, it will panic.
// otherwise, it simply returns nil and leaves the tokens unchanged.
func (p *Parser) tryParseLambdaExpression(must bool) (expr *FunctionExpression) {
	defer func() {
		if must && expr == nil {
			panic("bad lambda expression")
		}
	}()

	p.enter()

	params := &Parameters{}

	if _, ok := p.match(ttLeftParen); ok {
		for {
			if p.follow(ttIdentifier) {
				params.PutParam(p.expect(ttIdentifier).str)
			} else {
				if p.follow(ttRightParen) {
					break
				} else {
					p.leave(true)
					return nil
				}
			}
			if p.skip(ttComma) {
				continue
			}
		}
		if _, ok := p.match(ttRightParen); !ok {
			p.leave(true)
			return nil
		}
	} else {
		if !p.follow(ttIdentifier) {
			p.leave(true)
			return nil
		}
		params.PutParam(p.next().str)
	}

	if !p.follow(ttLambda) {
		p.leave(true)
		return nil
	}

	p.leave(false)
	p.next()

	var block *BlockStatement

	if p.follow(ttLeftBrace) {
		block = p.parseBlockStatement()
	} else {
		expr := p.parseExpression(ttQuestion)
		ret := NewReturnStatement(expr)
		block = NewBlockStatement(ret)
	}

	// a lambda expression is just an anonymous function
	return &FunctionExpression{
		name:   "",
		params: params,
		block:  block,
	}
}

func (p *Parser) tryParseIndexExpression(left Expression) (expr Expression) {
	switch token := p.next(); token.typ {
	case ttDot:
		if ident := p.next(); ident.typ == ttIdentifier {
			return &IndexExpression{
				indexable: left,
				key:       ValueFromString(ident.str),
			}
		}
	case ttLeftBracket:
		keyExpr := p.parseExpression(ttQuestion)
		if keyExpr == nil {
			return nil
		}
		if bracket := p.next(); bracket.typ != ttRightBracket {
			return nil
		}
		return &IndexExpression{
			indexable: left,
			key:       keyExpr,
		}
	default:
		p.undo(token)
	}
	return nil
}

func (p *Parser) tryParseCallExpression(left Expression) Expression {
	if paren := p.next(); paren.typ != ttLeftParen {
		p.undo(paren)
		return nil
	}

	call := CallExpression{
		Callable: left,
		Args:     &Arguments{},
	}

	if !p.follow(ttRightParen) {
		for {
			arg := p.parseExpression(ttQuestion)
			call.Args.PutArgument(arg)
			sep := p.next()
			if sep.typ == ttComma {
				continue
			} else if sep.typ == ttRightParen {
				p.undo(sep)
				break
			} else {
				panicf("unexpected token: %v", sep)
			}
		}
	}

	p.expect(ttRightParen)

	return &call
}

func (p *Parser) parseFunctionExpression() *FunctionExpression {
	var name string
	var block *BlockStatement
	params := &Parameters{}

	if p.follow(ttIdentifier) {
		name = p.next().str
	}

	p.expect(ttLeftParen)
	if !p.follow(ttRightParen) {
		for {
			name := p.expect(ttIdentifier).str
			params.PutParam(name)
			sep := p.next()
			if sep.typ == ttComma {
				continue
			} else if sep.typ == ttRightParen {
				p.undo(sep)
				break
			} else {
				panicf("unexpected token: %v", sep)
			}
		}
	}
	p.expect(ttRightParen)

	if !p.follow(ttLeftBrace) {
		panic("function needs a body")
	}

	saveBreakCount := p.breakCount
	p.breakCount = 0

	block = p.parseBlockStatement()

	p.breakCount = saveBreakCount

	return &FunctionExpression{
		name:   name,
		params: params,
		block:  block,
	}
}

func (p *Parser) parseObjectExpression() Expression {
	objexpr := NewObjectExpression()

	p.expect(ttLeftBrace)

	for {
		if p.follow(ttRightBrace) {
			break
		}

		var key string
		var expr Expression

		switch token := p.next(); token.typ {
		case ttString:
			key = token.str
		case ttIdentifier:
			key = token.str
		default:
			panic("unsupported key type")
		}

		p.expect(ttColon)

		expr = p.parseExpression(ttQuestion)
		objexpr.props[key] = expr

		// allow last comma
		p.skip(ttComma)
		if p.follow(ttRightBrace) {
			break
		}
	}

	p.expect(ttRightBrace)

	return objexpr
}

func (p *Parser) parseArrayExpression() Expression {
	arrExpr := NewArrayExpression()

	p.expect(ttLeftBracket)

	for {
		if p.follow(ttRightBracket) {
			break
		}

		elem := p.parseExpression(ttQuestion)
		arrExpr.elements = append(arrExpr.elements, elem)
		// allow last comma
		p.skip(ttComma)
		if p.follow(ttRightBracket) {
			break
		}
	}

	p.expect(ttRightBracket)

	return arrExpr
}
