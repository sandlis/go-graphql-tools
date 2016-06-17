package tools

import (
	"reflect"
	"strings"
	"unicode"

	"github.com/arvitaly/graphql"
)

type Resolver interface {
	IsResolve(sourceType reflect.Type, field reflect.StructField) bool
	Resolve(FieldInfo, graphql.ResolveParams) (interface{}, error)
}
type Generator struct {
	Types    map[reflect.Type]graphql.Output
	Resolver Resolver
}

func NewGenerator(resolver Resolver) *Generator {
	generator := Generator{}
	generator.Types = map[reflect.Type]graphql.Output{}
	generator.Resolver = resolver
	return &generator
}
func (generator *Generator) Generate(typ interface{}) interface{} {
	return generator._GenerateGraphqlObject(typ)
}
func (generator *Generator) GenerateObject(typ interface{}) *graphql.Object {
	return generator.Generate(typ).(*graphql.Object)
}

type FieldInfo struct {
	Name       string
	Type       reflect.Type
	Source     interface{}
	Args       interface{}
	ResolveTag string
}

//Generate graphql fields and interface by fields of struct
func (generator *Generator) getFields(source interface{}, sourceType reflect.Type) (graphql.Fields, []*graphql.Interface) {
	var graphqlFields = graphql.Fields{} //init graphql fields
	var graphqlInterfaces = []*graphql.Interface{}

	for i := 0; i < sourceType.NumField(); i++ {
		field := sourceType.Field(i)

		//Get graphql tag for field
		var sourceFieldGraphqlTag = field.Tag.Get("graphql")

		////Exclude field
		if sourceFieldGraphqlTag == "-" {
			continue
		}
		//////

		fieldType := field.Type

		if field.Anonymous && sourceFieldGraphqlTag == "" {
			graphqlFieldsExt, graphqlInterfacesExt := generator.getFields(reflect.ValueOf(source).Field(i).Interface(), fieldType)

			for key, value := range graphqlFieldsExt {
				graphqlFields[key] = value
			}
			for _, value := range graphqlInterfacesExt {
				graphqlInterfaces = append(graphqlInterfaces, value)
			}

		} else {

			sourceFieldGraphqlTagParams := strings.Split(sourceFieldGraphqlTag, ",")
			var graphqlTagType string
			if len(sourceFieldGraphqlTagParams) > 0 {
				graphqlTagType = strings.ToLower(sourceFieldGraphqlTagParams[0])
			}

			//init new field
			var graphqlField = &graphql.Field{}

			//

			var fieldName = field.Name

			graphqlField.Type = generator.getGraphQLType(fieldType, graphqlTagType, fieldName)

			if graphqlField.Type == nil {
				continue
			}
			///Args
			var argsI interface{} = nil
			if method, ok := sourceType.MethodByName("ArgsFor" + fieldName); ok {
				args := method.Func.Call([]reflect.Value{reflect.ValueOf(source)})[0]
				graphqlField.Args = generator.getArgs(args)
				argsI = args.Interface()
			}

			//Resolve
			resolveTag := sourceType.Field(i).Tag.Get("resolve")
			if generator.Resolver != nil && generator.Resolver.IsResolve(sourceType, field) {
				fieldInfo := FieldInfo{
					Name:       fieldName,
					Type:       fieldType,
					Source:     source,
					Args:       argsI,
					ResolveTag: resolveTag,
				}
				graphqlField.Resolve = func(p graphql.ResolveParams) (interface{}, error) {
					return generator.Resolver.Resolve(fieldInfo, p)
				}
			}

			graphqlField.Name = lA(fieldName)
			descriptionTag := sourceType.Field(i).Tag.Get("description")
			if descriptionTag == "-" {
				graphqlField.Description = ""
			} else {
				if descriptionTag == "" {
					graphqlField.Description = fieldName
				} else {
					graphqlField.Description = descriptionTag
				}
			}

			if sourceFieldGraphqlTag == "interface" {
				switch graphqlField.Type.(type) {
				case *graphql.Interface:
					graphqlInterfaces = append(graphqlInterfaces, graphqlField.Type.(*graphql.Interface))
					break
				default:
					panic("Invalid interface for type " + sourceType.Name() + ", " + field.Name + " is not interface, " + graphqlField.Type.String())
				}

			} else {
				graphqlFields[lA(fieldName)] = graphqlField
			}
		}
	}
	return graphqlFields, graphqlInterfaces
}

func (generator *Generator) _GenerateGraphqlObject(source interface{}) graphql.Output {
	types := generator.Types

	sourceType := reflect.TypeOf(source)
	//get name
	var name = sourceType.Name()

	//get description
	method, ok := sourceType.MethodByName("Description")
	var description string
	if ok {
		description = method.Func.Call([]reflect.Value{reflect.ValueOf(source)})[0].Interface().(string)
	} else {
		description = name
	}

	//If type is interface
	IsInterface := false
	if method, ok := sourceType.MethodByName("IsInterface"); ok {
		IsInterface = method.Func.Call([]reflect.Value{reflect.ValueOf(source)})[0].Interface().(bool)
	}

	//get fields
	graphqlFields, graphqlInterfaces := generator.getFields(source, sourceType)

	config := graphql.ObjectConfig{
		Name:        name,
		Description: description,
	}
	if len(graphqlFields) > 0 {

		config.Fields = graphqlFields
	}
	if len(graphqlInterfaces) > 0 {
		IsInterface = false
		config.Interfaces = graphqlInterfaces
	}

	var obj graphql.Output
	if IsInterface {
		obj = graphql.NewInterface(graphql.InterfaceConfig{
			Name:        config.Name,
			Fields:      config.Fields,
			Description: config.Description,
			ResolveType: generator.ResolveType,
		})
	} else {
		obj = graphql.NewObject(config)
	}

	types[sourceType] = obj

	return obj
}
func (generator *Generator) ResolveType(p graphql.ResolveTypeParams) *graphql.Object {
	return generator.Types[reflect.TypeOf(p.Value)].(*graphql.Object)
}

/**/
func (generator *Generator) getArgs(sourceValue reflect.Value) graphql.FieldConfigArgument {
	args := graphql.FieldConfigArgument{}
	sourceType := sourceValue.Type()
	for i := 0; i < sourceType.NumField(); i++ {
		field := sourceType.Field(i)
		if !sourceValue.Field(i).CanInterface() {
			continue
		}

		description := field.Name
		descriptionTag := field.Tag.Get("description")
		if descriptionTag == "-" {
			description = ""
		} else {
			if descriptionTag != "" {
				description = descriptionTag
			}
		}

		args[lA(field.Name)] = &graphql.ArgumentConfig{
			Type:         generator.getGraphQLType(field.Type, field.Tag.Get("graphql"), field.Name),
			Description:  description,
			DefaultValue: sourceValue.Field(i).Interface(),
		}
	}
	return args
}
func (generator *Generator) getGraphQLType(fieldType reflect.Type, graphqlTagType string, fieldName string) graphql.Output {
	types := generator.Types

	fieldKind := fieldType.Kind()
	var isNull = false
	if fieldKind == reflect.Ptr {
		isNull = true
		fieldKind = fieldType.Elem().Kind()
		fieldType = fieldType.Elem()
	}
	if graphqlTagType == "id" {
		if isNull {
			return graphql.ID
		} else {
			return graphql.NewNonNull(graphql.ID)
		}

	}
	if graphqlTagType == "input" {
		var configInput = graphql.InputObjectConfig{}

		inputType := generator._GenerateGraphqlObject(reflect.New(fieldType).Elem().Interface())
		configInput.Name = inputType.Name()
		configInput.Description = fieldName
		inputFields := graphql.InputObjectConfigFieldMap{}
		for key, value := range inputType.(*graphql.Object).Fields() {
			inputFields[key] = &graphql.InputObjectFieldConfig{
				Type: value.Type,
			}
		}
		configInput.Fields = inputFields

		typ := graphql.NewInputObject(configInput)

		if isNull {
			return typ
		} else {
			return graphql.NewNonNull(typ)
		}
	}

	if graphqlTagType == "enum" {
		var configMap = graphql.EnumValueConfigMap{}

		if method, ok := fieldType.MethodByName("Values"); ok {
			res := method.Func.Call([]reflect.Value{reflect.New(fieldType).Elem()})
			for _, key := range res[0].MapKeys() {
				configMap[key.String()] = &graphql.EnumValueConfig{Value: res[0].MapIndex(key).Interface()}
			}
		}
		typ := graphql.NewEnum(graphql.EnumConfig{
			Name:   fieldType.Name(),
			Values: configMap})
		if isNull {
			return typ
		} else {
			return graphql.NewNonNull(typ)
		}

	}
	kind := fieldType.Kind()
	if kind == reflect.Struct {

		if fieldObj, ok := types[fieldType]; ok {
			return fieldObj
		} else {
			return generator._GenerateGraphqlObject(reflect.New(fieldType).Elem().Interface())
		}
	}
	var graphqlType graphql.Output
	switch kind {
	case reflect.String:
		graphqlType = graphql.String
	case reflect.Int, reflect.Int32, reflect.Int64, reflect.Uint:
		graphqlType = graphql.Int
	case reflect.Float32, reflect.Float64:
		graphqlType = graphql.Float
	case reflect.Bool:
		graphqlType = graphql.Boolean
	case reflect.Slice:
		t := generator.getGraphQLType(fieldType.Elem(), graphqlTagType, fieldName)
		graphqlType = graphql.NewList(t)
	default:

		return nil

	}

	if !isNull {
		return graphql.NewNonNull(graphqlType)
	}
	return graphqlType
}

func lA(s string) string {
	a := []rune(s)
	a[0] = unicode.ToLower(a[0])
	return string(a)
}
func lU(s string) string {
	a := []rune(s)
	a[0] = unicode.ToUpper(a[0])
	return string(a)
}
