package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

type ScarConverter struct {
	fset        *token.FileSet
	indentLevel int
	output      strings.Builder
	imports     []string
	inFunction  bool
	inStruct    bool
	currentFunc string
}

func NewScarConverter() *ScarConverter {
	return &ScarConverter{
		fset:        token.NewFileSet(),
		indentLevel: 0,
	}
}

func (c *ScarConverter) indent() string {
	return strings.Repeat("    ", c.indentLevel)
}

func (c *ScarConverter) write(s string) {
	c.output.WriteString(s)
}

func (c *ScarConverter) writeln(s string) {
	c.output.WriteString(c.indent() + s + "\n")
}

func (c *ScarConverter) writeRaw(s string) {
	c.output.WriteString(s)
}

// Convert Go types to Scar types
func (c *ScarConverter) convertType(expr ast.Expr) string {
	if expr == nil {
		return ""
	}

	switch t := expr.(type) {
	case *ast.Ident:
		switch t.Name {
		case "int", "int32":
			return "int"
		case "int64":
			return "i64"
		case "string":
			return "string"
		case "bool":
			return "bool"
		case "byte":
			return "char"
		case "float32", "float64":
			return "float"
		default:
			return t.Name
		}
	case *ast.ArrayType:
		elemType := c.convertType(t.Elt)
		return fmt.Sprintf("list[%s]", elemType)
	case *ast.MapType:
		keyType := c.convertType(t.Key)
		valueType := c.convertType(t.Value)
		return fmt.Sprintf("map[%s: %s]", keyType, valueType)
	case *ast.StarExpr:
		baseType := c.convertType(t.X)
		return fmt.Sprintf("ref %s", baseType)
	case *ast.SelectorExpr:
		return c.convertExpr(t)
	}
	return "unknown"
}

// Convert Go expressions to Scar expressions
func (c *ScarConverter) convertExpr(expr ast.Expr) string {
	if expr == nil {
		return ""
	}

	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.BasicLit:
		if e.Kind == token.STRING {
			return e.Value
		}
		return e.Value
	case *ast.BinaryExpr:
		left := c.convertExpr(e.X)
		right := c.convertExpr(e.Y)
		op := e.Op.String()

		// Convert Go operators to Scar operators
		switch op {
		case "&&":
			op = "&&"
		case "||":
			op = "||"
		case "!=":
			op = "!="
		case "==":
			op = "=="
		}
		return fmt.Sprintf("%s %s %s", left, op, right)
	case *ast.UnaryExpr:
		operand := c.convertExpr(e.X)
		op := e.Op.String()
		return fmt.Sprintf("%s%s", op, operand)
	case *ast.CallExpr:
		return c.convertCallExpr(e)
	case *ast.SelectorExpr:
		x := c.convertExpr(e.X)
		return fmt.Sprintf("%s.%s", x, e.Sel.Name)
	case *ast.IndexExpr:
		x := c.convertExpr(e.X)
		index := c.convertExpr(e.Index)
		return fmt.Sprintf("%s[%s]", x, index)
	case *ast.CompositeLit:
		return c.convertCompositeLit(e)
	case *ast.TypeAssertExpr:
		x := c.convertExpr(e.X)
		typ := c.convertType(e.Type)
		return fmt.Sprintf("(%s)%s", typ, x)
	}
	return "# unknown expression"
}

func (c *ScarConverter) convertCallExpr(call *ast.CallExpr) string {
	var funcName string

	switch fn := call.Fun.(type) {
	case *ast.Ident:
		funcName = fn.Name

		// Handle special Go functions
		switch funcName {
		case "fmt.Printf", "printf":
			if len(call.Args) > 0 {
				format := c.convertExpr(call.Args[0])
				args := make([]string, 0, len(call.Args)-1)
				for i := 1; i < len(call.Args); i++ {
					args = append(args, c.convertExpr(call.Args[i]))
				}
				if len(args) > 0 {
					return fmt.Sprintf("print %s | %s", format, strings.Join(args, ", "))
				} else {
					return fmt.Sprintf("print %s", format)
				}
			}
		case "fmt.Println", "println":
			if len(call.Args) > 0 {
				arg := c.convertExpr(call.Args[0])
				return fmt.Sprintf("print %s", arg)
			}
		case "make":
			if len(call.Args) > 0 {
				typ := c.convertType(call.Args[0])
				if strings.HasPrefix(typ, "list[") {
					if len(call.Args) > 1 {
						size := c.convertExpr(call.Args[1])
						return fmt.Sprintf("new %s(%s)", typ, size)
					}
					return fmt.Sprintf("new %s()", typ)
				} else if strings.HasPrefix(typ, "map[") {
					return "[]"
				}
			}
		case "len":
			if len(call.Args) > 0 {
				arg := c.convertExpr(call.Args[0])
				return fmt.Sprintf("len(%s)", arg)
			}
		case "append":
			if len(call.Args) >= 2 {
				slice := c.convertExpr(call.Args[0])
				elem := c.convertExpr(call.Args[1])
				return fmt.Sprintf("%s.add(%s)", slice, elem)
			}
		}
	case *ast.SelectorExpr:
		obj := c.convertExpr(fn.X)
		method := fn.Sel.Name
		funcName = fmt.Sprintf("%s.%s", obj, method)

		// Handle fmt.Println and fmt.Printf when called as selector expressions
		if obj == "fmt" {
			switch method {
			case "Printf":
				if len(call.Args) > 0 {
					format := c.convertExpr(call.Args[0])
					args := make([]string, 0, len(call.Args)-1)
					for i := 1; i < len(call.Args); i++ {
						args = append(args, c.convertExpr(call.Args[i]))
					}
					if len(args) > 0 {
						return fmt.Sprintf("print %s | %s", format, strings.Join(args, ", "))
					} else {
						return fmt.Sprintf("print %s", format)
					}
				}
			case "Println":
				if len(call.Args) > 0 {
					arg := c.convertExpr(call.Args[0])
					return fmt.Sprintf("print %s", arg)
				}
			}
		}
	}

	// Convert arguments
	args := make([]string, len(call.Args))
	for i, arg := range call.Args {
		args[i] = c.convertExpr(arg)
	}

	return fmt.Sprintf("%s(%s)", funcName, strings.Join(args, ", "))
}

func (c *ScarConverter) convertCompositeLit(lit *ast.CompositeLit) string {
	if lit.Type != nil {
		typ := c.convertType(lit.Type)
		if strings.HasPrefix(typ, "list[") {
			elements := make([]string, len(lit.Elts))
			for i, elt := range lit.Elts {
				elements[i] = c.convertExpr(elt)
			}
			return fmt.Sprintf("[%s]", strings.Join(elements, ", "))
		}
	}
	return "# composite literal"
}

func (c *ScarConverter) convertStmt(stmt ast.Stmt) {
	if stmt == nil {
		return
	}

	switch s := stmt.(type) {
	case *ast.ExprStmt:
		c.writeln(c.convertExpr(s.X))
	case *ast.AssignStmt:
		c.convertAssignStmt(s)
	case *ast.DeclStmt:
		c.convertDeclStmt(s)
	case *ast.IfStmt:
		c.convertIfStmt(s)
	case *ast.ForStmt:
		c.convertForStmt(s)
	case *ast.RangeStmt:
		c.convertRangeStmt(s)
	case *ast.ReturnStmt:
		c.convertReturnStmt(s)
	case *ast.BlockStmt:
		c.convertBlockStmt(s)
	case *ast.IncDecStmt:
		c.convertIncDecStmt(s)
	case *ast.SwitchStmt:
		c.convertSwitchStmt(s)
	case *ast.TypeSwitchStmt:
		c.writeln("# type switch not supported */")
	case *ast.CaseClause:
		c.convertCaseClause(s)
	case *ast.BranchStmt:
		c.convertBranchStmt(s)
	default:
		c.writeln("# unknown statement")
	}
}

func (c *ScarConverter) convertAssignStmt(stmt *ast.AssignStmt) {
	if len(stmt.Lhs) == 1 && len(stmt.Rhs) == 1 {
		lhs := c.convertExpr(stmt.Lhs[0])
		rhs := c.convertExpr(stmt.Rhs[0])

		switch stmt.Tok {
		case token.ASSIGN:
			c.writeln(fmt.Sprintf("%s = %s", lhs, rhs))
		case token.DEFINE:
			// Try to infer type from RHS
			c.writeln(fmt.Sprintf("%s = %s", lhs, rhs))
		case token.ADD_ASSIGN:
			c.writeln(fmt.Sprintf("%s = %s + %s", lhs, lhs, rhs))
		case token.SUB_ASSIGN:
			c.writeln(fmt.Sprintf("%s = %s - %s", lhs, lhs, rhs))
		default:
			c.writeln(fmt.Sprintf("%s = %s", lhs, rhs))
		}
	}
}

func (c *ScarConverter) convertDeclStmt(stmt *ast.DeclStmt) {
	if genDecl, ok := stmt.Decl.(*ast.GenDecl); ok {
		for _, spec := range genDecl.Specs {
			if valueSpec, ok := spec.(*ast.ValueSpec); ok {
				for i, name := range valueSpec.Names {
					var typ string
					if valueSpec.Type != nil {
						typ = c.convertType(valueSpec.Type)
					}

					if len(valueSpec.Values) > i {
						val := c.convertExpr(valueSpec.Values[i])
						if typ != "" {
							c.writeln(fmt.Sprintf("%s %s = %s", typ, name.Name, val))
						} else {
							c.writeln(fmt.Sprintf("%s = %s", name.Name, val))
						}
					} else {
						if typ != "" {
							c.writeln(fmt.Sprintf("%s %s", typ, name.Name))
						}
					}
				}
			}
		}
	}
}

func (c *ScarConverter) convertIfStmt(stmt *ast.IfStmt) {
	if stmt.Init != nil {
		c.convertStmt(stmt.Init)
	}

	cond := c.convertExpr(stmt.Cond)
	c.writeln(fmt.Sprintf("if %s:", cond))
	c.indentLevel++
	c.convertBlockStmt(stmt.Body)
	c.indentLevel--

	if stmt.Else != nil {
		if elseIf, ok := stmt.Else.(*ast.IfStmt); ok {
			// Handle else if -> elif conversion
			c.convertElseIfStmt(elseIf)
		} else if elseBlock, ok := stmt.Else.(*ast.BlockStmt); ok {
			c.writeln("else:")
			c.indentLevel++
			c.convertBlockStmt(elseBlock)
			c.indentLevel--
		}
	}
}

func (c *ScarConverter) convertElseIfStmt(stmt *ast.IfStmt) {
	if stmt.Init != nil {
		c.convertStmt(stmt.Init)
	}

	cond := c.convertExpr(stmt.Cond)
	c.writeln(fmt.Sprintf("elif %s:", cond))
	c.indentLevel++
	c.convertBlockStmt(stmt.Body)
	c.indentLevel--

	if stmt.Else != nil {
		if elseIf, ok := stmt.Else.(*ast.IfStmt); ok {
			// Recursively handle nested else if
			c.convertElseIfStmt(elseIf)
		} else if elseBlock, ok := stmt.Else.(*ast.BlockStmt); ok {
			c.writeln("else:")
			c.indentLevel++
			c.convertBlockStmt(elseBlock)
			c.indentLevel--
		}
	}
}

func (c *ScarConverter) convertForStmt(stmt *ast.ForStmt) {
	if stmt.Init != nil && stmt.Cond != nil && stmt.Post != nil {
		// Traditional for loop
		init := strings.TrimSpace(c.convertStmtToString(stmt.Init))
		cond := c.convertExpr(stmt.Cond)
		post := strings.TrimSpace(c.convertStmtToString(stmt.Post))

		// Extract variable from init (simplified)
		initParts := strings.Split(init, " ")
		if len(initParts) >= 3 {
			varName := initParts[len(initParts)-3]
			c.writeln(fmt.Sprintf("for %s; %s; %s:", varName, cond, post))
		} else {
			c.writeln(fmt.Sprintf("while %s:", cond))
		}
	} else if stmt.Cond != nil {
		// While loop
		cond := c.convertExpr(stmt.Cond)
		c.writeln(fmt.Sprintf("while %s:", cond))
	} else {
		// Infinite loop
		c.writeln("while true:")
	}

	c.indentLevel++
	c.convertBlockStmt(stmt.Body)
	c.indentLevel--
}

func (c *ScarConverter) convertRangeStmt(stmt *ast.RangeStmt) {
	x := c.convertExpr(stmt.X)

	if stmt.Key != nil && stmt.Value != nil {
		key := c.convertExpr(stmt.Key)
		value := c.convertExpr(stmt.Value)
		c.writeln(fmt.Sprintf("for %s, %s in %s:", key, value, x))
	} else if stmt.Key != nil {
		key := c.convertExpr(stmt.Key)
		c.writeln(fmt.Sprintf("for %s in %s:", key, x))
	}

	c.indentLevel++
	c.convertBlockStmt(stmt.Body)
	c.indentLevel--
}

func (c *ScarConverter) convertReturnStmt(stmt *ast.ReturnStmt) {
	if len(stmt.Results) == 0 {
		c.writeln("return")
	} else if len(stmt.Results) == 1 {
		result := c.convertExpr(stmt.Results[0])
		c.writeln(fmt.Sprintf("return %s", result))
	} else {
		results := make([]string, len(stmt.Results))
		for i, result := range stmt.Results {
			results[i] = c.convertExpr(result)
		}
		c.writeln(fmt.Sprintf("return %s", strings.Join(results, ", ")))
	}
}

func (c *ScarConverter) convertBlockStmt(stmt *ast.BlockStmt) {
	for _, s := range stmt.List {
		c.convertStmt(s)
	}
}

func (c *ScarConverter) convertIncDecStmt(stmt *ast.IncDecStmt) {
	x := c.convertExpr(stmt.X)
	if stmt.Tok == token.INC {
		c.writeln(fmt.Sprintf("%s = %s + 1", x, x))
	} else {
		c.writeln(fmt.Sprintf("%s = %s - 1", x, x))
	}
}

func (c *ScarConverter) convertSwitchStmt(stmt *ast.SwitchStmt) {
	if stmt.Init != nil {
		c.convertStmt(stmt.Init)
	}

	if stmt.Tag != nil {
		tag := c.convertExpr(stmt.Tag)
		c.writeln(fmt.Sprintf("switch %s:", tag))
	} else {
		c.writeln("switch:")
	}

	c.indentLevel++
	c.convertBlockStmt(stmt.Body)
	c.indentLevel--
}

func (c *ScarConverter) convertCaseClause(stmt *ast.CaseClause) {
	if stmt.List == nil {
		c.writeln("default:")
	} else {
		cases := make([]string, len(stmt.List))
		for i, expr := range stmt.List {
			cases[i] = c.convertExpr(expr)
		}
		c.writeln(fmt.Sprintf("case %s:", strings.Join(cases, ", ")))
	}

	c.indentLevel++
	for _, s := range stmt.Body {
		c.convertStmt(s)
	}
	c.indentLevel--
}

func (c *ScarConverter) convertBranchStmt(stmt *ast.BranchStmt) {
	switch stmt.Tok {
	case token.BREAK:
		c.writeln("break")
	case token.CONTINUE:
		c.writeln("continue")
	}
}

func (c *ScarConverter) convertStmtToString(stmt ast.Stmt) string {
	oldOutput := c.output
	oldIndent := c.indentLevel

	c.output = strings.Builder{}
	c.indentLevel = 0
	c.convertStmt(stmt)
	result := strings.TrimSpace(c.output.String())

	c.output = oldOutput
	c.indentLevel = oldIndent

	return result
}

func (c *ScarConverter) convertFuncDecl(decl *ast.FuncDecl) {
	// Special handling for main function - convert to top-level statements
	if decl.Name.Name == "main" && decl.Recv == nil {
		// Convert main function body to top-level statements
		if decl.Body != nil {
			c.convertBlockStmt(decl.Body)
		}
		return
	}

	var receiver string
	if decl.Recv != nil && len(decl.Recv.List) > 0 {
		// Method
		field := decl.Recv.List[0]
		if len(field.Names) > 0 {
			receiver = field.Names[0].Name
		}
		recvType := c.convertType(field.Type)
		if strings.HasPrefix(recvType, "ref ") {
			recvType = strings.TrimPrefix(recvType, "ref ")
		}
		receiver = fmt.Sprintf("this %s", recvType)
	}

	// Function name
	funcName := decl.Name.Name

	// Parameters
	var params []string
	if decl.Type.Params != nil {
		for _, field := range decl.Type.Params.List {
			paramType := c.convertType(field.Type)
			for _, name := range field.Names {
				params = append(params, fmt.Sprintf("%s %s", paramType, name.Name))
			}
		}
	}

	// Return type
	var returnType string
	if decl.Type.Results != nil && len(decl.Type.Results.List) > 0 {
		if len(decl.Type.Results.List) == 1 && len(decl.Type.Results.List[0].Names) <= 1 {
			returnType = " -> " + c.convertType(decl.Type.Results.List[0].Type)
		}
	}

	// Build function signature
	if receiver != "" {
		c.writeln(fmt.Sprintf("fn %s(%s)%s:", funcName, strings.Join(params, ", "), returnType))
	} else {
		c.writeln(fmt.Sprintf("fn %s(%s)%s:", funcName, strings.Join(params, ", "), returnType))
	}

	// Function body
	if decl.Body != nil {
		c.indentLevel++
		c.convertBlockStmt(decl.Body)
		c.indentLevel--
	}
	c.writeln("")
}

func (c *ScarConverter) convertTypeSpec(spec *ast.TypeSpec) {
	switch t := spec.Type.(type) {
	case *ast.StructType:
		c.writeln(fmt.Sprintf("class %s:", spec.Name.Name))
		c.indentLevel++

		// Constructor
		if t.Fields != nil && len(t.Fields.List) > 0 {
			c.writeln("init:")
			c.indentLevel++
			for _, field := range t.Fields.List {
				fieldType := c.convertType(field.Type)
				for _, name := range field.Names {
					c.writeln(fmt.Sprintf("%s this.%s", fieldType, name.Name))
				}
			}
			c.indentLevel--
		} else {
			c.writeln("init:")
		}

		c.indentLevel--
		c.writeln("")
	case *ast.InterfaceType:
		c.writeln(fmt.Sprintf("interface %s:", spec.Name.Name))
		c.indentLevel++
		if t.Methods != nil {
			for _, method := range t.Methods.List {
				if len(method.Names) > 0 {
					methodName := method.Names[0].Name
					if funcType, ok := method.Type.(*ast.FuncType); ok {
						// Convert method signature
						var params []string
						if funcType.Params != nil {
							for _, param := range funcType.Params.List {
								paramType := c.convertType(param.Type)
								for _, name := range param.Names {
									params = append(params, fmt.Sprintf("%s %s", paramType, name.Name))
								}
							}
						}
						var returnType string
						if funcType.Results != nil && len(funcType.Results.List) > 0 {
							returnType = " -> " + c.convertType(funcType.Results.List[0].Type)
						}
						c.writeln(fmt.Sprintf("fn %s(%s)%s", methodName, strings.Join(params, ", "), returnType))
					}
				}
			}
		}
		c.indentLevel--
		c.writeln("")
	}
}

func (c *ScarConverter) convertImports(decl *ast.GenDecl) {
	for _, spec := range decl.Specs {
		if importSpec, ok := spec.(*ast.ImportSpec); ok {
			path := strings.Trim(importSpec.Path.Value, "\"")

			// Convert common Go standard library imports
			scarImport := c.convertImportPath(path)
			if scarImport != "" {
				c.imports = append(c.imports, scarImport)
			}
		}
	}
}

func (c *ScarConverter) convertImportPath(path string) string {
	switch path {
	case "crypto/sha256":
	case "crypto/sha512":
	case "crypto/sha1":
	case "crypto/md5":
		return "std/crypto"
	case "io":
	case "bufio":
		return "std/io"
	case "json":
		return "std/json"
	case "regexp":
		return "std/regex"
	case "os":
		return "std/os"
	case "strings":
		return "std/strings"
	case "strconv":
		return "std/strings"
	case "math":
		return "std/math"
	case "time":
		return "std/time"
	case "math/rand":
		return "std/random"
	default:
		return ""
	}
	return ""
}

func (c *ScarConverter) ConvertFile(filename string) (string, error) {
	src, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}

	file, err := parser.ParseFile(c.fset, filename, src, parser.ParseComments)
	if err != nil {
		return "", err
	}

	// Process imports
	for _, decl := range file.Decls {
		if genDecl, ok := decl.(*ast.GenDecl); ok && genDecl.Tok == token.IMPORT {
			c.convertImports(genDecl)
		}
	}

	// Write imports
	for _, imp := range c.imports {
		c.writeln(fmt.Sprintf("import \"%s\"", imp))
	}
	if len(c.imports) > 0 {
		c.writeln("")
	}

	// Process declarations
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			c.convertFuncDecl(d)
		case *ast.GenDecl:
			switch d.Tok {
			case token.TYPE:
				for _, spec := range d.Specs {
					if typeSpec, ok := spec.(*ast.TypeSpec); ok {
						c.convertTypeSpec(typeSpec)
					}
				}
			case token.VAR, token.CONST:
				for _, spec := range d.Specs {
					if valueSpec, ok := spec.(*ast.ValueSpec); ok {
						for i, name := range valueSpec.Names {
							var typ string
							if valueSpec.Type != nil {
								typ = c.convertType(valueSpec.Type)
							}

							if len(valueSpec.Values) > i {
								val := c.convertExpr(valueSpec.Values[i])
								if typ != "" {
									c.writeln(fmt.Sprintf("%s %s = %s", typ, name.Name, val))
								} else {
									c.writeln(fmt.Sprintf("%s = %s", name.Name, val))
								}
							} else {
								if typ != "" {
									c.writeln(fmt.Sprintf("%s %s", typ, name.Name))
								}
							}
						}
					}
				}
				c.writeln("")
			}
		}
	}

	return c.output.String(), nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: gts <input.go> [output.scar]")
		os.Exit(1)
	}

	inputFile := os.Args[1]
	outputFile := strings.TrimSuffix(inputFile, ".go") + ".scar"
	if len(os.Args) > 2 {
		outputFile = os.Args[2]
	}

	converter := NewScarConverter()
	result, err := converter.ConvertFile(inputFile)
	if err != nil {
		fmt.Printf("Error converting file: %v\n", err)
		os.Exit(1)
	}

	err = os.WriteFile(outputFile, []byte(result), 0644)
	if err != nil {
		fmt.Printf("Error writing output file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully converted %s to %s\n", inputFile, outputFile)
}
