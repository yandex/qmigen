package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"go/ast"
	"go/format"
	"go/token"

	"github.com/hjson/hjson-go"
	"github.com/pascaldekloe/name"
)

type QMIService struct {
	Name string
	Type string
}

type QMIClient struct {
	Name  string
	Type  string
	Since string
}

type QMIMessageIDEnum struct {
	Name string
	Type string
}

type QMIIndicationIDEnum struct {
	Name string
	Type string
}

type QMIMessage struct {
	Name    string
	Type    string
	Service string
	ID      string `json:"id"`
	Since   string
	Input   []QMITLV
	Output  []QMITLV
}

type QMIIndication struct {
	Name string
	Type string
}

type QMITLVField struct {
	Name         string
	Format       string
	Contents     []QMITLVField // type={struct,sequence}
	ArrayElement *QMITLVField  `json:"array-element"`     // type=array
	IntSize      int           `json:"guint-size,string"` // type=guint-sized
	PublicFormat string        `json:"public-format"`
	CommonRef    string        `json:"common-ref"`
}

type QMITLV struct {
	Type  string
	ID    string `json:"id"`
	Since string
	QMITLVField
}

type QMIPrerequisite struct {
	Type      string
	Field     string
	Operation string
	Value     string
}

var CommonIdents = map[string]*ast.Ident{}

func init() {
	for _, ident := range []string{
		"_", "nil",
		"panic",
		"int", "byte", "uint8", "uint16", "uint32", "uint64", "int8", "int16", "int32", "int64", "string",
		"qmi",
		"make", "String",
		"dev", "Device", "Send",
		"m", "msg", "Message",
		"service", "Service", "ServiceID", "MessageID",
		"registerMessage", "Message",
		"findTag",
		"msg", "input", "output",
		"err", "error",
		"w", "io", "write", "Write", "Writer", "TLVWriteTo", "WriteTo",
		"r", "Read", "Reader", "ReadFrom", "Uint16",
		"b", "buf", "bytes", "Buffer", "Len",
		"TLVsWriteTo", "TLVsReadFrom",
		"tlv", "binary", "LittleEndian",
		"fmt", "Errorf",
		"OperationResult",
	} {
		CommonIdents[ident] = ast.NewIdent(ident)
	}
}

var CommonRefs = map[string]map[string]interface{}{}
var CommonSize = map[string]int{
	"nil":    0,
	"int":    8,
	"byte":   1,
	"uint8":  1,
	"int8":   1,
	"uint16": 2,
	"int16":  2,
	"uint32": 4,
	"int32":  4,
	"uint64": 8,
	"int64":  8,
	"string": -1,
}

type QMIEntity interface {
	Register(*ast.File) error
}

func (qs *QMIService) Register(f *ast.File) error {
	typ := &ast.GenDecl{
		Tok:    token.TYPE,
		TokPos: f.Pos() - 1,
		Specs: []ast.Spec{
			&ast.TypeSpec{
				Name: ast.NewIdent("QMIService" + name.CamelCase(qs.Name, true)),
				Type: &ast.StructType{
					Fields: &ast.FieldList{
						List: []*ast.Field{},
					},
				},
			},
		},
	}
	fun := &ast.FuncDecl{
		Recv: &ast.FieldList{
			List: []*ast.Field{
				&ast.Field{
					Names: []*ast.Ident{CommonIdents["service"]},
					Type:  &ast.StarExpr{X: typ.Specs[0].(*ast.TypeSpec).Name},
				},
			},
		},
		Name: CommonIdents["ServiceID"],
		Type: &ast.FuncType{
			Params: &ast.FieldList{},
			Results: &ast.FieldList{
				List: []*ast.Field{
					&ast.Field{
						Type: CommonIdents["Service"],
					},
				},
			},
		},
		Body: &ast.BlockStmt{
			List: []ast.Stmt{
				&ast.ReturnStmt{
					Results: []ast.Expr{
						ast.NewIdent("QMI_SERVICE_" + qs.Name),
					},
				},
			},
		},
	}
	f.Decls = append(f.Decls, typ, fun)

	return nil
}

func (qc *QMIClient) Register(f *ast.File) error {
	return nil
}

func (qmie *QMIMessageIDEnum) Register(f *ast.File) error {
	return nil
}

func (qiie *QMIIndicationIDEnum) Register(f *ast.File) error {
	return nil
}

func (qm *QMIMessage) Register(f *ast.File) error {
	inputs := &ast.GenDecl{
		Tok:    token.TYPE,
		TokPos: f.Pos() - 1,
		Specs: []ast.Spec{
			&ast.TypeSpec{
				Name: ast.NewIdent(qm.Service + name.CamelCase(qm.Name, true) + "Input"),
				Type: &ast.StructType{
					Fields: &ast.FieldList{
						List: []*ast.Field{},
					},
				},
			},
		},
	}

	outputs := &ast.GenDecl{
		Tok:    token.TYPE,
		TokPos: f.Pos() - 1,
		Specs: []ast.Spec{
			&ast.TypeSpec{
				Name: ast.NewIdent(qm.Service + name.CamelCase(qm.Name, true) + "Output"),
				Type: &ast.StructType{
					Fields: &ast.FieldList{
						List: []*ast.Field{},
					},
				},
			},
		},
	}

	n := 0

	input_sizes := make([]int, len(qm.Input))
	for i, input := range qm.Input {
		typ, n1, err := parseType(input.QMITLVField)
		if err != nil {
			return err
		}
		input_sizes[i] = n1
		field := &ast.Field{
			Type: typ,
		}
		if input.Name != "" {
			field.Names = []*ast.Ident{ast.NewIdent(name.CamelCase(input.Name, true))}
		}
		inputs.Specs[0].(*ast.TypeSpec).Type.(*ast.StructType).Fields.List = append(
			inputs.Specs[0].(*ast.TypeSpec).Type.(*ast.StructType).Fields.List,
			field,
		)
		if n != -1 {
			if n1 >= 0 {
				n += n1 + 2 + 1
			} else {
				n = -1
			}
		}
	}

	has_op_result := false
	output_sizes := make([]int, len(qm.Output))
	for i, output := range qm.Output {
		if output.CommonRef == "Operation Result" {
			has_op_result = true
		}
		typ, n1, err := parseType(output.QMITLVField)
		if err != nil {
			return err
		}
		output_sizes[i] = n1
		if output.Name != "" {
			outputs.Specs[0].(*ast.TypeSpec).Type.(*ast.StructType).Fields.List = append(
				outputs.Specs[0].(*ast.TypeSpec).Type.(*ast.StructType).Fields.List,
				&ast.Field{
					Names: []*ast.Ident{ast.NewIdent(name.CamelCase(output.Name, true))},
					Type:  typ,
				},
			)
		} else {
			outputs.Specs[0].(*ast.TypeSpec).Type.(*ast.StructType).Fields.List = append(
				outputs.Specs[0].(*ast.TypeSpec).Type.(*ast.StructType).Fields.List,
				&ast.Field{
					Type: typ,
				},
			)
		}
	}

	fun_id := &ast.FuncDecl{
		Recv: &ast.FieldList{
			List: []*ast.Field{
				&ast.Field{
					Names: []*ast.Ident{CommonIdents["msg"]},
					Type:  inputs.Specs[0].(*ast.TypeSpec).Name,
				},
			},
		},
		Name: CommonIdents["MessageID"],
		Type: &ast.FuncType{
			Params: &ast.FieldList{},
			Results: &ast.FieldList{
				List: []*ast.Field{
					&ast.Field{
						Type: CommonIdents["uint16"],
					},
				},
			},
		},
		Body: &ast.BlockStmt{
			List: []ast.Stmt{
				&ast.ReturnStmt{
					Results: []ast.Expr{
						&ast.BasicLit{
							Kind:  token.INT,
							Value: qm.ID,
						},
					},
				},
			},
		},
	}

	fun_service_id := &ast.FuncDecl{
		Recv: &ast.FieldList{
			List: []*ast.Field{
				&ast.Field{
					Names: []*ast.Ident{CommonIdents["msg"]},
					Type:  inputs.Specs[0].(*ast.TypeSpec).Name,
				},
			},
		},
		Name: CommonIdents["ServiceID"],
		Type: &ast.FuncType{
			Params: &ast.FieldList{},
			Results: &ast.FieldList{
				List: []*ast.Field{
					&ast.Field{
						Type: CommonIdents["Service"],
					},
				},
			},
		},
		Body: &ast.BlockStmt{
			List: []ast.Stmt{
				&ast.ReturnStmt{
					Results: []ast.Expr{
						ast.NewIdent("QMI_SERVICE_" + qm.Service),
					},
				},
			},
		},
	}

	fun_id_output := &ast.FuncDecl{
		Recv: &ast.FieldList{
			List: []*ast.Field{
				&ast.Field{
					Names: []*ast.Ident{CommonIdents["msg"]},
					Type:  outputs.Specs[0].(*ast.TypeSpec).Name,
				},
			},
		},
		Name: fun_id.Name,
		Type: fun_id.Type,
		Body: fun_id.Body,
	}

	fun_service_id_output := &ast.FuncDecl{
		Recv: &ast.FieldList{
			List: []*ast.Field{
				&ast.Field{
					Names: []*ast.Ident{CommonIdents["msg"]},
					Type:  outputs.Specs[0].(*ast.TypeSpec).Name,
				},
			},
		},
		Name: fun_service_id.Name,
		Type: fun_service_id.Type,
		Body: fun_service_id.Body,
	}

	fun := &ast.FuncDecl{
		Recv: &ast.FieldList{
			List: []*ast.Field{
				&ast.Field{
					Names: []*ast.Ident{CommonIdents["dev"]},
					Type:  &ast.StarExpr{X: CommonIdents["Device"]},
				},
			},
		},
		Name: ast.NewIdent(qm.Service + name.CamelCase(qm.Name, true)),
		Type: &ast.FuncType{
			Params: &ast.FieldList{
				List: []*ast.Field{
					&ast.Field{
						Names: []*ast.Ident{CommonIdents["input"]},
						Type:  inputs.Specs[0].(*ast.TypeSpec).Name,
					},
				},
			},
			Results: &ast.FieldList{
				List: []*ast.Field{
					&ast.Field{
						Names: []*ast.Ident{CommonIdents["m"]},
						Type:  &ast.StarExpr{X: outputs.Specs[0].(*ast.TypeSpec).Name},
					},
					&ast.Field{
						Names: []*ast.Ident{CommonIdents["err"]},
						Type:  CommonIdents["error"],
					},
				},
			},
		},
		Body: &ast.BlockStmt{
			List: []ast.Stmt{
				&ast.DeclStmt{
					Decl: &ast.GenDecl{
						Tok: token.VAR,
						Specs: []ast.Spec{
							&ast.ValueSpec{
								Names: []*ast.Ident{CommonIdents["msg"]},
								Type:  CommonIdents["Message"],
							},
						},
					},
				},
				&ast.AssignStmt{
					Lhs: []ast.Expr{
						CommonIdents["msg"],
						CommonIdents["err"],
					},
					Tok: token.ASSIGN,
					Rhs: []ast.Expr{
						&ast.CallExpr{
							Fun: &ast.SelectorExpr{
								X:   CommonIdents["dev"],
								Sel: CommonIdents["Send"],
							},
							Args: []ast.Expr{
								CommonIdents["input"],
							},
						},
					},
				},
				handleErr(),
				&ast.AssignStmt{
					Lhs: []ast.Expr{
						CommonIdents["m"],
					},
					Tok: token.ASSIGN,
					Rhs: []ast.Expr{
						&ast.TypeAssertExpr{
							X: CommonIdents["msg"],
							Type: &ast.StarExpr{
								X: outputs.Specs[0].(*ast.TypeSpec).Name,
							},
						},
					},
				},
				&ast.ReturnStmt{},
			},
		},
	}

	tlv_write_stmts := []ast.Stmt{
		&ast.DeclStmt{
			Decl: &ast.GenDecl{
				Tok: token.VAR,
				Specs: []ast.Spec{
					&ast.ValueSpec{
						Names: []*ast.Ident{CommonIdents["buf"]},
						Type: &ast.StarExpr{
							X: &ast.SelectorExpr{
								X:   CommonIdents["bytes"],
								Sel: CommonIdents["Buffer"],
							},
						},
					},
				},
			},
		},
		&ast.AssignStmt{
			Lhs: []ast.Expr{
				CommonIdents["_"],
			},
			Tok: token.ASSIGN,
			Rhs: []ast.Expr{
				CommonIdents["buf"],
			},
		},
	}

	for i, input := range qm.Input {
		write_stmts, err := input.GenWriteTo(CommonIdents["msg"], input_sizes[i])
		if err != nil {
			return err
		}
		tlv_write_stmts = append(
			tlv_write_stmts,
			write_stmts...,
		)
	}
	tlv_write_stmts = append(tlv_write_stmts, &ast.ReturnStmt{
		Results: []ast.Expr{
			CommonIdents["nil"],
		},
	})

	fun_tlvs_writeTo := &ast.FuncDecl{
		Recv: &ast.FieldList{
			List: []*ast.Field{
				&ast.Field{
					Names: []*ast.Ident{CommonIdents["msg"]},
					Type:  inputs.Specs[0].(*ast.TypeSpec).Name,
				},
			},
		},
		Name: CommonIdents["TLVsWriteTo"],
		Type: &ast.FuncType{
			Params: &ast.FieldList{
				List: []*ast.Field{
					&ast.Field{
						Names: []*ast.Ident{CommonIdents["w"]},
						Type: &ast.SelectorExpr{
							X:   CommonIdents["io"],
							Sel: CommonIdents["Writer"],
						},
					},
				},
			},
			Results: &ast.FieldList{
				List: []*ast.Field{
					&ast.Field{
						Names: []*ast.Ident{CommonIdents["err"]},
						Type:  CommonIdents["error"],
					},
				},
			},
		},
		Body: &ast.BlockStmt{
			List: tlv_write_stmts,
		},
	}

	fun_tlvs_writeTo_output := &ast.FuncDecl{
		Recv: &ast.FieldList{
			List: []*ast.Field{
				&ast.Field{
					Names: []*ast.Ident{CommonIdents["msg"]},
					Type:  outputs.Specs[0].(*ast.TypeSpec).Name,
				},
			},
		},
		Name: fun_tlvs_writeTo.Name,
		Type: fun_tlvs_writeTo.Type,
		Body: &ast.BlockStmt{
			List: []ast.Stmt{
				&ast.ExprStmt{
					X: &ast.CallExpr{
						Fun: CommonIdents["panic"],
						Args: []ast.Expr{
							&ast.BasicLit{
								Kind:  token.STRING,
								Value: `"not implemented"`,
							},
						},
					},
				},
			},
		},
	}

	tlv_read_stmts := []ast.Stmt{
		&ast.DeclStmt{
			Decl: &ast.GenDecl{
				Tok: token.VAR,
				Specs: []ast.Spec{
					&ast.ValueSpec{
						Names: []*ast.Ident{CommonIdents["b"]},
						Type: &ast.StarExpr{
							X: &ast.SelectorExpr{
								X:   CommonIdents["bytes"],
								Sel: CommonIdents["Buffer"],
							},
						},
					},
				},
			},
		},
	}

	for i, output := range qm.Output {
		read_stmts, err := output.GenReadFrom(CommonIdents["msg"], output_sizes[i])
		if err != nil {
			return err
		}
		tlv_read_stmts = append(
			tlv_read_stmts,
			read_stmts...,
		)
	}

	tlv_read_stmts = append(
		tlv_read_stmts,
		&ast.ReturnStmt{
			Results: []ast.Expr{
				CommonIdents["nil"],
			},
		},
	)

	fun_tlvs_readFrom_out := &ast.FuncDecl{
		Recv: &ast.FieldList{
			List: []*ast.Field{
				&ast.Field{
					Names: []*ast.Ident{CommonIdents["msg"]},
					Type: &ast.StarExpr{
						X: outputs.Specs[0].(*ast.TypeSpec).Name,
					},
				},
			},
		},
		Name: CommonIdents["TLVsReadFrom"],
		Type: &ast.FuncType{
			Params: &ast.FieldList{
				List: []*ast.Field{
					&ast.Field{
						Names: []*ast.Ident{CommonIdents["r"]},
						Type: &ast.StarExpr{
							X: &ast.SelectorExpr{
								X:   CommonIdents["bytes"],
								Sel: CommonIdents["Buffer"],
							},
						},
					},
				},
			},
			Results: &ast.FieldList{
				List: []*ast.Field{
					&ast.Field{
						Names: []*ast.Ident{CommonIdents["err"]},
						Type:  CommonIdents["error"],
					},
				},
			},
		},
		Body: &ast.BlockStmt{
			List: tlv_read_stmts,
		},
	}

	fun_tlvs_readFrom := &ast.FuncDecl{
		Recv: &ast.FieldList{
			List: []*ast.Field{
				&ast.Field{
					Names: []*ast.Ident{CommonIdents["msg"]},
					Type:  inputs.Specs[0].(*ast.TypeSpec).Name,
				},
			},
		},
		Name: fun_tlvs_readFrom_out.Name,
		Type: fun_tlvs_readFrom_out.Type,
		Body: &ast.BlockStmt{
			List: []ast.Stmt{
				&ast.ExprStmt{
					X: &ast.CallExpr{
						Fun: CommonIdents["panic"],
						Args: []ast.Expr{
							&ast.BasicLit{
								Kind:  token.STRING,
								Value: `"not implemented"`,
							},
						},
					},
				},
			},
		},
	}

	f.Decls = append(
		f.Decls,
		inputs, outputs,
		fun,
		fun_service_id, fun_id,
		fun_service_id_output, fun_id_output,
		fun_tlvs_readFrom, fun_tlvs_readFrom_out,
		fun_tlvs_writeTo, fun_tlvs_writeTo_output,
	)

	if has_op_result {
		f.Decls = append(
			f.Decls,
			&ast.FuncDecl{
				Recv: &ast.FieldList{
					List: []*ast.Field{
						&ast.Field{
							Names: []*ast.Ident{CommonIdents["msg"]},
							Type: &ast.StarExpr{
								X: outputs.Specs[0].(*ast.TypeSpec).Name,
							},
						},
					},
				},
				Name: CommonIdents["OperationResult"],
				Type: &ast.FuncType{
					Params: &ast.FieldList{},
					Results: &ast.FieldList{
						List: []*ast.Field{
							&ast.Field{
								Type: CommonIdents["QMIStructOperationResult"],
							},
						},
					},
				},
				Body: &ast.BlockStmt{
					List: []ast.Stmt{
						&ast.ReturnStmt{
							Results: []ast.Expr{
								&ast.SelectorExpr{
									X:   CommonIdents["msg"],
									Sel: CommonIdents["QMIStructOperationResult"],
								},
							},
						},
					},
				},
			},
		)
	}

	return nil
}

func (qi *QMIIndication) Register(f *ast.File) error {
	return nil
}

func (qt *QMITLV) GenTypeDecl() (*ast.GenDecl, int, error) {
	n := 0
	fieldList := []*ast.Field{}

	for _, field := range qt.Contents {
		typ, n1, err := parseType(field)
		if err != nil {
			return nil, 0, err
		}
		fieldList = append(fieldList, &ast.Field{
			Names: []*ast.Ident{
				ast.NewIdent(name.CamelCase(field.Name, true)),
			},
			Type: typ,
		})
		if n != -1 {
			if n1 == -1 {
				n = -1
			} else {
				n += n1
			}
		}
	}

	if len(qt.Contents) == 0 {
		typ, n1, err := parseType((*qt).QMITLVField)
		if err != nil {
			return nil, 0, err
		}
		n = n1
		field := &ast.Field{
			Type: typ,
		}
		if qt.Name != "" {
			field.Names = []*ast.Ident{
				ast.NewIdent(name.CamelCase(qt.Name, true)),
			}
		}
		fieldList = append(fieldList, field)
	}

	CommonSize[qt.Name] = n

	t := &ast.GenDecl{
		Tok: token.TYPE,
		Specs: []ast.Spec{
			&ast.TypeSpec{
				Name: ast.NewIdent("QMIStruct" + name.CamelCase(qt.Name, true)),
				Type: &ast.StructType{
					Fields: &ast.FieldList{
						List: fieldList,
					},
				},
			},
		},
	}

	return t, n, nil
}

func (field *QMITLVField) GenReadFromPayload(parent ast.Expr) ([]ast.Stmt, error) {
	ident := ast.NewIdent(name.CamelCase(field.Name, true))
	switch strings.TrimPrefix(field.Format, "g") {
	case "", "array":
		// TODO
		return []ast.Stmt{}, nil
	case "uint-sized":
		buf_name := ast.NewIdent("buf_" + name.SnakeCase(field.Name))
		return []ast.Stmt{
			&ast.AssignStmt{
				Lhs: []ast.Expr{
					buf_name,
				},
				Tok: token.DEFINE,
				Rhs: []ast.Expr{
					&ast.CallExpr{
						Fun: CommonIdents["make"],
						Args: []ast.Expr{
							&ast.ArrayType{
								Elt: CommonIdents["byte"],
							},
							&ast.BasicLit{
								Kind:  token.INT,
								Value: strconv.Itoa(field.IntSize),
							},
						},
					},
				},
			},
			&ast.ExprStmt{
				X: &ast.CallExpr{
					Fun: &ast.SelectorExpr{
						X:   CommonIdents["r"],
						Sel: CommonIdents["Read"],
					},
					Args: []ast.Expr{
						buf_name,
					},
				},
			},
			&ast.AssignStmt{
				Lhs: []ast.Expr{
					&ast.SelectorExpr{
						X:   parent,
						Sel: ident,
					},
				},
				Tok: token.ASSIGN,
				Rhs: []ast.Expr{
					buf_name,
				},
			},
		}, nil

	case "int8", "uint8", "byte", "int16", "uint16", "int32", "uint32", "uint64":
		return []ast.Stmt{
			&ast.ExprStmt{
				X: &ast.CallExpr{
					Fun: &ast.SelectorExpr{
						X:   CommonIdents["binary"],
						Sel: CommonIdents["Read"],
					},
					Args: []ast.Expr{
						CommonIdents["b"],
						&ast.SelectorExpr{
							X:   CommonIdents["binary"],
							Sel: CommonIdents["LittleEndian"],
						},
						&ast.UnaryExpr{
							Op: token.AND,
							X: &ast.SelectorExpr{
								X:   parent,
								Sel: ident,
							},
						},
					},
				},
			},
		}, nil
	case "string":
		return []ast.Stmt{
			&ast.AssignStmt{
				Lhs: []ast.Expr{
					&ast.SelectorExpr{
						X:   parent,
						Sel: ident,
					},
				},
				Tok: token.ASSIGN,
				Rhs: []ast.Expr{
					&ast.CallExpr{
						Fun: &ast.SelectorExpr{
							X:   CommonIdents["b"],
							Sel: CommonIdents["String"],
						},
						Args: []ast.Expr{},
					},
				},
			},
		}, nil
	case "sequence":
		var stmts []ast.Stmt
		if _, ok := CommonRefs[field.Name]; !ok {
			parent = &ast.SelectorExpr{
				X:   parent,
				Sel: ident,
			}
		}
		for _, sub_field := range field.Contents {
			field_stmts, err := sub_field.GenReadFromPayload(parent)
			if err != nil {
				return nil, err
			}
			stmts = append(stmts, field_stmts...)
		}
		return stmts, nil
	case "struct":
		var stmts []ast.Stmt
		if _, ok := CommonRefs[field.Name]; !ok {
			parent = &ast.SelectorExpr{
				X:   parent,
				Sel: ident,
			}
		}
		for _, field := range field.Contents {
			field_stmts, err := field.GenReadFromPayload(parent)
			if err != nil {
				return nil, err
			}
			stmts = append(stmts, field_stmts...)
		}
		return stmts, nil
	default:
		return nil, fmt.Errorf("format %q is unsupported", field.Format)
	}
}

func (field *QMITLVField) GenWriteToPayload(parent ast.Expr, writer ast.Expr) ([]ast.Stmt, error) {
	ident := ast.NewIdent(name.CamelCase(field.Name, true))
	switch strings.TrimPrefix(field.Format, "g") {
	case "":
		// TODO: support common-ref
		return []ast.Stmt{}, nil
	case "byte", "int8", "uint8", "uint16", "uint32", "uint64", "int16", "int32":
		return []ast.Stmt{
			&ast.AssignStmt{
				Lhs: []ast.Expr{CommonIdents["err"]},
				Tok: token.ASSIGN,
				Rhs: []ast.Expr{
					&ast.CallExpr{
						Fun: &ast.SelectorExpr{
							X:   CommonIdents["binary"],
							Sel: CommonIdents["Write"],
						},
						Args: []ast.Expr{
							writer,
							&ast.SelectorExpr{
								X:   CommonIdents["binary"],
								Sel: CommonIdents["LittleEndian"],
							},
							&ast.SelectorExpr{
								X:   parent,
								Sel: ident,
							},
						},
					},
				},
			},
			handleErr(),
		}, nil
	case "string":
		return []ast.Stmt{
			&ast.AssignStmt{
				Lhs: []ast.Expr{
					CommonIdents["_"],
					CommonIdents["err"],
				},
				Tok: token.ASSIGN,
				Rhs: []ast.Expr{
					&ast.CallExpr{
						Fun: &ast.SelectorExpr{
							X:   writer,
							Sel: CommonIdents["Write"],
						},
						Args: []ast.Expr{
							&ast.CallExpr{
								Fun: &ast.ArrayType{
									Elt: CommonIdents["byte"],
								},
								Args: []ast.Expr{
									&ast.SelectorExpr{
										X:   parent,
										Sel: ident,
									},
								},
							},
						},
					},
				},
			},
			handleErr(),
		}, nil
	case "sequence":
		var stmts []ast.Stmt
		if _, ok := CommonRefs[field.Name]; !ok {
			parent = &ast.SelectorExpr{
				X:   parent,
				Sel: ident,
			}
		}
		for _, field := range field.Contents {
			field_stmts, err := field.GenWriteToPayload(
				parent,
				writer,
			)
			if err != nil {
				return nil, err
			}
			stmts = append(stmts, field_stmts...)
		}
		return stmts, nil
	case "struct":
		var stmts []ast.Stmt
		if _, ok := CommonRefs[field.Name]; !ok {
			parent = &ast.SelectorExpr{
				X:   parent,
				Sel: ident,
			}
		}
		for _, field := range field.Contents {
			field_stmts, err := field.GenWriteToPayload(parent, writer)
			if err != nil {
				return nil, err
			}
			stmts = append(stmts, field_stmts...)
		}
		return stmts, nil
	case "array":
		return []ast.Stmt{}, nil // TODO
	default:
		return nil, fmt.Errorf("format %q is unsupported", field.Format)
	}
}

func (qt *QMITLV) GenReadFrom(parent ast.Expr, n int) ([]ast.Stmt, error) {
	var stmts []ast.Stmt
	id := qt.ID
	if id == "" { // HACK
		id = "2"
	}
	stmts = append(
		stmts,
		&ast.AssignStmt{
			Lhs: []ast.Expr{
				CommonIdents["b"],
			},
			Tok: token.ASSIGN,
			Rhs: []ast.Expr{
				&ast.CallExpr{
					Fun: CommonIdents["findTag"],
					Args: []ast.Expr{
						CommonIdents["r"],
						&ast.BasicLit{
							Kind:  token.INT,
							Value: id,
						},
					},
				},
			},
		},
	)
	read_data, err := qt.GenReadFromPayload(parent)
	if err != nil {
		return nil, err
	}
	check_b := &ast.IfStmt{
		Cond: &ast.BinaryExpr{
			X:  CommonIdents["b"],
			Op: token.NEQ,
			Y:  CommonIdents["nil"],
		},
		Body: &ast.BlockStmt{List: read_data},
	}
	if id == "2" {
		check_b.Else = &ast.BlockStmt{
			List: []ast.Stmt{
				&ast.AssignStmt{
					Lhs: []ast.Expr{CommonIdents["err"]},
					Tok: token.ASSIGN,
					Rhs: []ast.Expr{
						&ast.CallExpr{
							Fun: &ast.SelectorExpr{
								X:   CommonIdents["fmt"],
								Sel: CommonIdents["Errorf"],
							},
							Args: []ast.Expr{
								&ast.BasicLit{
									Kind:  token.STRING,
									Value: `"cannot find tag 2"`,
								},
							},
						},
					},
				},
				&ast.ReturnStmt{},
			},
		}
	}
	stmts = append(
		stmts,
		check_b,
	)
	return stmts, nil
}

func handleErr() ast.Stmt {
	return &ast.IfStmt{
		Cond: &ast.BinaryExpr{
			X:  CommonIdents["err"],
			Op: token.NEQ,
			Y:  CommonIdents["nil"],
		},
		Body: &ast.BlockStmt{
			List: []ast.Stmt{
				&ast.ReturnStmt{},
			},
		},
	}
}

func (qt *QMITLV) GenWriteTo(parent ast.Expr, n int) ([]ast.Stmt, error) {
	write_tag := &ast.AssignStmt{
		Lhs: []ast.Expr{CommonIdents["_"], CommonIdents["err"]},
		Tok: token.ASSIGN,
		Rhs: []ast.Expr{
			&ast.CallExpr{
				Fun: &ast.SelectorExpr{
					X:   CommonIdents["w"],
					Sel: CommonIdents["Write"],
				},
				Args: []ast.Expr{
					&ast.CompositeLit{
						Type: &ast.ArrayType{
							Elt: CommonIdents["byte"],
						},
						Elts: []ast.Expr{
							&ast.BasicLit{
								Kind:  token.INT,
								Value: qt.ID,
							},
						},
					},
				},
			},
		},
	}
	if n >= 0 {
		write_data, err := qt.GenWriteToPayload(parent, CommonIdents["w"])
		if err != nil {
			return nil, err
		}
		write_length := &ast.AssignStmt{
			Lhs: []ast.Expr{CommonIdents["err"]},
			Tok: token.ASSIGN,
			Rhs: []ast.Expr{
				&ast.CallExpr{
					Fun: &ast.SelectorExpr{
						X:   CommonIdents["binary"],
						Sel: CommonIdents["Write"],
					},
					Args: []ast.Expr{
						CommonIdents["w"],
						&ast.SelectorExpr{
							X:   CommonIdents["binary"],
							Sel: CommonIdents["LittleEndian"],
						},
						&ast.CallExpr{
							Fun: CommonIdents["uint16"],
							Args: []ast.Expr{
								&ast.BasicLit{
									Kind:  token.INT,
									Value: strconv.Itoa(n),
								},
							},
						},
					},
				},
			},
		}
		return append([]ast.Stmt{
			write_tag,
			handleErr(),
			write_length,
			handleErr(),
		},
			write_data...,
		), nil
	} else {
		n := qt.Name
		if n == "" {
			n = qt.CommonRef
		}
		buffer := ast.NewIdent("buf_" + name.SnakeCase(n))
		make_buffer := &ast.AssignStmt{
			Lhs: []ast.Expr{buffer},
			Tok: token.DEFINE,
			Rhs: []ast.Expr{
				&ast.UnaryExpr{
					Op: token.AND,
					X: &ast.CompositeLit{
						Type: &ast.SelectorExpr{
							X:   CommonIdents["bytes"],
							Sel: CommonIdents["Buffer"],
						},
					},
				},
			},
		}
		write_data, err := qt.GenWriteToPayload(parent, buffer)
		if err != nil {
			return nil, err
		}
		write_length := &ast.AssignStmt{
			Lhs: []ast.Expr{CommonIdents["err"]},
			Tok: token.ASSIGN,
			Rhs: []ast.Expr{
				&ast.CallExpr{
					Fun: &ast.SelectorExpr{
						X:   CommonIdents["binary"],
						Sel: CommonIdents["Write"],
					},
					Args: []ast.Expr{
						CommonIdents["w"],
						&ast.SelectorExpr{
							X:   CommonIdents["binary"],
							Sel: CommonIdents["LittleEndian"],
						},
						&ast.CallExpr{
							Fun: CommonIdents["uint16"],
							Args: []ast.Expr{
								&ast.CallExpr{
									Fun: &ast.SelectorExpr{
										X:   buffer,
										Sel: CommonIdents["Len"],
									},
								},
							},
						},
					},
				},
			},
		}
		flush_buf := &ast.AssignStmt{
			Lhs: []ast.Expr{
				CommonIdents["_"],
				CommonIdents["err"],
			},
			Tok: token.ASSIGN,
			Rhs: []ast.Expr{
				&ast.CallExpr{
					Fun: &ast.SelectorExpr{
						X:   buffer,
						Sel: CommonIdents["WriteTo"],
					},
					Args: []ast.Expr{
						CommonIdents["w"],
					},
				},
			},
		}
		return append(
			append(
				[]ast.Stmt{make_buffer, write_tag, handleErr()},
				write_data...,
			),
			write_length,
			handleErr(),
			flush_buf,
			handleErr(),
		), nil
	}
}

func (qt *QMITLV) GenReadFromFunc(t *ast.GenDecl, n int) (*ast.FuncDecl, error) {
	read_stmts, err := qt.GenReadFrom(CommonIdents["tlv"], n)
	if err != nil {
		return nil, err
	}

	return &ast.FuncDecl{
		Recv: &ast.FieldList{
			List: []*ast.Field{
				&ast.Field{
					Names: []*ast.Ident{CommonIdents["tlv"]},
					Type:  &ast.StarExpr{X: t.Specs[0].(*ast.TypeSpec).Name},
				},
			},
		},
		Name: CommonIdents["ReadFrom"],
		Type: &ast.FuncType{
			Params: &ast.FieldList{
				List: []*ast.Field{
					&ast.Field{
						Names: []*ast.Ident{CommonIdents["r"]},
						Type: &ast.StarExpr{
							X: &ast.SelectorExpr{
								X:   CommonIdents["bytes"],
								Sel: CommonIdents["Buffer"],
							},
						},
					},
				},
			},
			Results: &ast.FieldList{
				List: []*ast.Field{
					&ast.Field{
						Names: []*ast.Ident{CommonIdents["err"]},
						Type:  CommonIdents["error"],
					},
				},
			},
		},
		Body: &ast.BlockStmt{
			List: append(
				append(
					[]ast.Stmt{
						&ast.DeclStmt{
							Decl: &ast.GenDecl{
								Tok: token.VAR,
								Specs: []ast.Spec{
									&ast.ValueSpec{
										Names: []*ast.Ident{CommonIdents["b"]},
										Type: &ast.StarExpr{
											X: &ast.SelectorExpr{
												X:   CommonIdents["bytes"],
												Sel: CommonIdents["Buffer"],
											},
										},
									},
								},
							},
						},
					},
					read_stmts...,
				),
				&ast.ReturnStmt{},
			),
		},
	}, nil
}

func (qt *QMITLV) Register(f *ast.File) error {
	t, n, err := qt.GenTypeDecl()
	if err != nil {
		return err
	}

	if n == 0 {
		return fmt.Errorf("bad TLV: %#v", qt)
	}

	fun_readFrom, err := qt.GenReadFromFunc(t, n)
	if err != nil {
		return err
	}

	f.Decls = append(f.Decls, t, fun_readFrom)
	return nil
}

func parseType(field QMITLVField) (ast.Expr, int, error) {
	switch field.Format {
	case "array":
		typ, _, err := parseType(*field.ArrayElement)
		if err != nil {
			return nil, 0, err
		}

		return &ast.ArrayType{Elt: typ}, -1, nil
	case "struct", "sequence":
		stype := &ast.StructType{
			Fields: &ast.FieldList{
				List: []*ast.Field{},
			},
		}
		n := 0
		for _, field := range field.Contents {
			typ, n1, err := parseType(field)
			if err != nil {
				return nil, 0, err
			}
			if n != -1 {
				n += n1
			}
			sfield := &ast.Field{
				Type: typ,
			}
			if field.Name != "" {
				sfield.Names = []*ast.Ident{
					ast.NewIdent(name.CamelCase(field.Name, true)),
				}
			}
			stype.Fields.List = append(stype.Fields.List, sfield)
		}

		return stype, n, nil
	case "guint-sized":
		return &ast.ArrayType{Elt: CommonIdents["byte"]}, field.IntSize, nil
	default:
		tname := strings.TrimPrefix(field.Format, "g")
		n, ok := CommonSize[tname]
		if !ok && field.CommonRef != "" {
			_, ok = CommonRefs[field.CommonRef]
			if ok {
				ident, ok := CommonIdents["QMIStruct"+name.CamelCase(field.CommonRef, true)]
				if ok {
					return ident, CommonSize[field.CommonRef], nil
				}
			}
		} else if ok {
			return ast.NewIdent(tname), n, nil
		}

		return nil, 0, fmt.Errorf("TLV format %q is not implemented yet: %#v", field.Format, field)
	}
}

func (qp *QMIPrerequisite) Register(f *ast.File) error {
	return nil
}

var QMIEntityMap = map[string]func() interface{}{
	"Service":            func() interface{} { return &QMIService{} },
	"Client":             func() interface{} { return &QMIClient{} },
	"Message-ID-Enum":    func() interface{} { return &QMIMessageIDEnum{} },
	"Indication-ID-Enum": func() interface{} { return &QMIIndicationIDEnum{} },
	"Message":            func() interface{} { return &QMIMessage{} },
	"Indication":         func() interface{} { return &QMIIndication{} },
	"TLV":                func() interface{} { return &QMITLV{} },
	"prerequisite":       func() interface{} { return &QMIPrerequisite{} },
}

type ErrUnexpectedType string

func (e ErrUnexpectedType) Error() string {
	return fmt.Sprintf("unexpected type: %s", string(e))
}

func addCommon(f *ast.File) {
	var declspec []ast.Spec
	for _, import_module := range []string{
		"bytes",
		"context",
		"encoding/binary",
		"fmt",
		"io",
		"log",
		"os",
		"sync",
		"syscall",
	} {
		spec := &ast.ImportSpec{
			Path: &ast.BasicLit{
				Kind:  token.STRING,
				Value: fmt.Sprintf("%q", import_module),
			},
		}
		f.Imports = append(f.Imports, spec)
		declspec = append(declspec, spec)
	}
	constspec := make([]ast.Spec, 0, len(ServiceMap)+1)
	constspec = append(constspec, &ast.ValueSpec{
		Names: []*ast.Ident{ast.NewIdent("QMI_SERVICE_UNKNOWN")},
		Type:  ast.NewIdent("Service"),
		Values: []ast.Expr{
			&ast.BasicLit{
				Kind:  token.INT,
				Value: "0xff",
			},
		},
	})
	var smap []ast.Expr
	var keys []int
	for i, _ := range ServiceMap {
		keys = append(keys, int(i))
	}
	sort.Ints(keys)
	for _, i := range keys {
		name := ServiceMap[Service(i)]
		key := fmt.Sprintf("QMI_SERVICE_%s", name)
		value := &ast.BasicLit{
			Kind:  token.INT,
			Value: strconv.Itoa(int(i)),
		}
		constspec = append(constspec, &ast.ValueSpec{
			Names: []*ast.Ident{ast.NewIdent(key)},
			Values: []ast.Expr{
				value,
			},
		})
		smap = append(smap, &ast.KeyValueExpr{
			Key: value,
			Value: &ast.BasicLit{
				Kind:  token.STRING,
				Value: fmt.Sprintf("%q", key),
			},
		})
	}
	varspec := []ast.Spec{
		&ast.ValueSpec{
			Names: []*ast.Ident{ast.NewIdent("ServiceMap")},
			Values: []ast.Expr{
				&ast.CompositeLit{
					Type: &ast.MapType{
						Key:   constspec[0].(*ast.ValueSpec).Type,
						Value: CommonIdents["string"],
					},
					Elts: smap,
				},
			},
		},
	}
	f.Decls = append([]ast.Decl{
		&ast.GenDecl{
			Tok:   token.IMPORT,
			Specs: declspec,
		},
		&ast.GenDecl{
			Tok:   token.CONST,
			Specs: constspec,
		},
		&ast.GenDecl{
			Tok:   token.VAR,
			Specs: varspec,
		},
	}, f.Decls...)
}

func convert(outputFile, inputFile string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	if !filepath.IsAbs(inputFile) {
		inputFile, err = filepath.Rel(
			filepath.Dir(filepath.Join(wd, outputFile)),
			filepath.Join(wd, inputFile),
		)
		if err != nil {
			panic(err)
		}
	}

	input, err := ioutil.ReadFile(inputFile)
	if err != nil {
		return err
	}

	var raw_entities []interface{}
	var entities []QMIEntity

	err = hjson.Unmarshal(input, &raw_entities)
	if err != nil {
		return err
	}

	fs := token.NewFileSet()
	f := &ast.File{
		Name:  CommonIdents["qmi"],
		Scope: ast.NewScope(nil),
	}

	for _, re := range raw_entities {
		typI, ok := re.(map[string]interface{})
		if !ok {
			return ErrUnexpectedType("not an object")
		}

		typS, ok := typI["type"].(string)
		if !ok {
			return ErrUnexpectedType("no \"type\" field")
		}

		cRef, ok := typI["common-ref"].(string)
		if ok {
			delete(typI, "common-ref")
			typI["name"] = cRef
			CommonRefs[cRef] = typI
			n := "QMIStruct" + name.CamelCase(cRef, true)
			CommonIdents[n] = ast.NewIdent(n)
			if typS == "TLV" {
				tlv := &QMITLV{}
				b, err := json.Marshal(re)
				if err != nil {
					return err
				}

				err = json.Unmarshal(b, tlv)
				if err != nil {
					return err
				}

				err = tlv.Register(f)
				if err != nil {
					return err
				}
			}
			continue
		}

		cons, ok := QMIEntityMap[typS]
		if !ok {
			return ErrUnexpectedType(typS)
		}

		entity := cons()

		b, err := json.Marshal(re)
		if err != nil {
			return err
		}

		err = json.Unmarshal(b, entity)
		if err != nil {
			return err
		}

		entity_impl := entity.(QMIEntity)

		err = entity_impl.Register(f)
		if err != nil {
			return fmt.Errorf("error processing %s: %w", typS, err)
		}

		entities = append(entities, entity_impl)
	}

	f_out, err := os.OpenFile(outputFile, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0666)
	if err != nil {
		return err
	}

	genpath, err := filepath.Abs(os.Args[0])
	if err != nil {
		genpath = os.Args[0]
	} else {
		genpath = filepath.Join(
			"..",
			filepath.Base(filepath.Dir(genpath)),
			filepath.Base(genpath),
		)
	}
	fmt.Fprintf(f_out, "//go:generate %s %s $GOFILE\n", genpath, inputFile)

	if filepath.Base(outputFile) == "qmi-common.go" {
		addCommon(f)
	} else {
		var declspec []ast.Spec
		for _, import_module := range []string{
			"bytes",
			"encoding/binary",
			"fmt",
			"io",
		} {
			spec := &ast.ImportSpec{
				Path: &ast.BasicLit{
					Kind:  token.STRING,
					Value: fmt.Sprintf("%q", import_module),
				},
			}
			f.Imports = append(f.Imports, spec)
			declspec = append(declspec, spec)
		}
		f.Decls = append([]ast.Decl{
			&ast.GenDecl{
				Tok:   token.IMPORT,
				Specs: declspec,
			},
		}, f.Decls...)
	}

	init_stmts := []ast.Stmt{}

	for _, entity := range entities {
		switch v := entity.(type) {
		case *QMIMessage:
			ident := ast.NewIdent(v.Service + name.CamelCase(v.Name, true) + "Output")

			flit := &ast.FuncLit{
				Type: &ast.FuncType{
					Results: &ast.FieldList{
						List: []*ast.Field{
							&ast.Field{
								Type: CommonIdents["Message"],
							},
						},
					},
				},
				Body: &ast.BlockStmt{
					List: []ast.Stmt{
						&ast.ReturnStmt{
							Results: []ast.Expr{
								&ast.UnaryExpr{
									Op: token.AND,
									X: &ast.CompositeLit{
										Type: ident,
									},
								},
							},
						},
					},
				},
			}

			init_stmts = append(
				init_stmts,
				&ast.ExprStmt{
					X: &ast.CallExpr{
						Fun: CommonIdents["registerMessage"],
						Args: []ast.Expr{
							flit,
						},
					},
				},
			)
		}
	}

	if len(init_stmts) > 0 {
		fun_init := &ast.FuncDecl{
			Name: ast.NewIdent("init"),
			Type: &ast.FuncType{
				Params: &ast.FieldList{},
			},
			Body: &ast.BlockStmt{
				List: init_stmts,
			},
		}

		f.Decls = append(f.Decls, fun_init)
	}

	// DEBUG: ast.Print(fs, f)

	defer f_out.Close()

	defer func() {
		fmt.Fprintf(
			f_out,
			"\n// Code generated by %s from %s, DO NOT EDIT.\n",
			genpath,
			inputFile,
		)

		if filepath.Base(outputFile) == "qmi-common.go" {
			f_out.Write([]byte(COMMON_FOOTER))
		}

		f_out.Write([]byte("// vim: ai:ts=8:sw=8:noet:syntax=go\n"))
	}()

	return format.Node(f_out, fs, f)
}

func main() {
	if len(os.Args) <= 1 {
		os.RemoveAll("../qmi")
		os.MkdirAll("../qmi", 0777)

		err := convert("../qmi/qmi-common.go", "data/qmi-common.json")
		if err != nil {
			panic(err)
		}

		err = convert("../qmi/qmi-service-ctl.go", "data/qmi-service-ctl.json")
		if err != nil {
			panic(err)
		}

		err = convert("../qmi/qmi-service-dms.go", "data/qmi-service-dms.json")
		if err != nil {
			panic(err)
		}

		err = convert("../qmi/qmi-service-wds.go", "data/qmi-service-wds.json")
		if err != nil {
			panic(err)
		}
	} else if len(os.Args) == 3 {
		wd, err := os.Getwd()
		if err != nil {
			panic(err)
		}

		dir := filepath.Dir(filepath.Join(wd, os.Args[1]))
		err = convert("/dev/null", filepath.Join(dir, "qmi-common.json"))
		if err != nil {
			panic(err)
		}

		err = convert(os.Args[2], os.Args[1])
		if err != nil {
			panic(err)
		}
	} else {
		panic(fmt.Sprintf("usage: %s [<inputFile> <outputFile>]", os.Args[0]))
	}
}

// vim: ai:ts=8:sw=8:noet:syntax=go
