package funcs

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/infracost/infracost/internal/hcl/mock"
	"github.com/turbot/terraform-components/lang/marks"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
	"github.com/zclconf/go-cty/cty/function"
)

// Adapted from https://github.com/zclconf/go-cty/blob/ea922e7a95ba2be57897697117f318670e066d22/cty/function/stdlib/conversion.go
// to handle Infracost mock values.
//
// MakeToFunc constructs a "to..." function, like "tostring", which converts
// its argument to a specific type or type kind.
//
// The given type wantTy can be any type constraint that cty's "convert" package
// would accept. In particular, this means that you can pass
// cty.List(cty.DynamicPseudoType) to mean "list of any single type", which
// will then cause cty to attempt to unify all of the element types when given
// a tuple.
func MakeToFunc(wantTy cty.Type) function.Function {
	return function.New(&function.Spec{
		Description: fmt.Sprintf("Converts the given value to %s, or raises an error if that conversion is impossible.", wantTy.FriendlyName()),
		Params: []function.Parameter{
			{
				Name: "v",
				// We use DynamicPseudoType rather than wantTy here so that
				// all values will pass through the function API verbatim and
				// we can handle the conversion logic within the Type and
				// Impl functions. This allows us to customize the error
				// messages to be more appropriate for an explicit type
				// conversion, whereas the cty function system produces
				// messages aimed at _implicit_ type conversions.
				Type:             cty.DynamicPseudoType,
				AllowNull:        true,
				AllowMarked:      true,
				AllowDynamicType: true,
			},
		},
		Type: func(args []cty.Value) (cty.Type, error) {
			gotTy := args[0].Type()
			if gotTy.Equals(wantTy) {
				return wantTy, nil
			}
			conv := convert.GetConversionUnsafe(args[0].Type(), wantTy)
			if conv == nil {
				// We'll use some specialized errors for some trickier cases,
				// but most we can handle in a simple way.
				switch {
				case gotTy.IsTupleType() && wantTy.IsTupleType():
					return cty.NilType, function.NewArgErrorf(0, "incompatible tuple type for conversion: %s", convert.MismatchMessage(gotTy, wantTy))
				case gotTy.IsObjectType() && wantTy.IsObjectType():
					return cty.NilType, function.NewArgErrorf(0, "incompatible object type for conversion: %s", convert.MismatchMessage(gotTy, wantTy))
				case gotTy == cty.String && wantTy.IsListType() || wantTy.IsTupleType() || wantTy.IsSetType() || wantTy.IsObjectType() || wantTy.IsMapType():
					val, _ := args[0].UnmarkDeep()
					strVal := val.AsString()
					if strings.HasSuffix(strVal, "-mock") || strings.Contains(strVal, mock.Identifier) {
						return wantTy, nil
					}
					return cty.NilType, function.NewArgErrorf(0, "cannot convert %s to %s", gotTy.FriendlyName(), wantTy.FriendlyNameForConstraint())
				default:
					return cty.NilType, function.NewArgErrorf(0, "cannot convert %s to %s", gotTy.FriendlyName(), wantTy.FriendlyNameForConstraint())
				}
			}
			// If a conversion is available then everything is fine.
			return wantTy, nil
		},
		Impl: func(args []cty.Value, retType cty.Type) (cty.Value, error) {
			// We didn't set "AllowUnknown" on our argument, so it is guaranteed
			// to be known here but may still be null.
			ret, err := convert.Convert(args[0], retType)
			if err != nil {
				// Because we used GetConversionUnsafe above, conversion can
				// still potentially fail in here. For example, if the user
				// asks to convert the string "a" to bool then we'll
				// optimistically permit it during type checking but fail here
				// once we note that the value isn't either "true" or "false".
				val, _ := args[0].UnmarkDeep()
				gotTy := val.Type()
				switch {
				case marks.Contains(args[0], marks.Sensitive):
					return cty.NilVal, function.NewArgErrorf(0, "cannot convert sensitive type %s to %s", gotTy.FriendlyName(), wantTy.FriendlyNameForConstraint())
				case gotTy == cty.String && wantTy == cty.Bool:
					what := "string"
					if !val.IsNull() {
						what = strconv.Quote(val.AsString())
					}
					return cty.NilVal, function.NewArgErrorf(0, `cannot convert %s to bool; only the strings "true" or "false" are allowed`, what)
				case gotTy == cty.String && wantTy == cty.Number:
					what := "string"
					if !val.IsNull() {
						what = strconv.Quote(val.AsString())
					}
					return cty.NilVal, function.NewArgErrorf(0, `cannot convert %s to number; given string must be a decimal representation of a number`, what)
				case gotTy == cty.String && wantTy.IsListType() || wantTy.IsTupleType() || wantTy.IsSetType() || wantTy.IsObjectType() || wantTy.IsMapType():
					strVal := val.AsString()
					if strings.HasSuffix(strVal, "-mock") || strings.Contains(strVal, mock.Identifier) {
						switch {
						case wantTy.IsListType():
							return cty.ListVal([]cty.Value{val}), nil
						case wantTy.IsTupleType():
							return cty.TupleVal([]cty.Value{val}), nil
						case wantTy.IsSetType():
							return cty.SetVal([]cty.Value{val}), nil
						case wantTy.IsObjectType():
							return cty.ObjectVal(map[string]cty.Value{}), nil
						case wantTy.IsMapType():
							return cty.MapVal(map[string]cty.Value{}), nil
						default:
							return cty.NilVal, function.NewArgErrorf(0, "cannot convert %s to %s", gotTy.FriendlyName(), wantTy.FriendlyNameForConstraint())
						}
					}

					return cty.NilVal, function.NewArgErrorf(0, "cannot convert %s to %s", gotTy.FriendlyName(), wantTy.FriendlyNameForConstraint())
				default:
					return cty.NilVal, function.NewArgErrorf(0, "cannot convert %s to %s", gotTy.FriendlyName(), wantTy.FriendlyNameForConstraint())
				}
			}
			return ret, nil
		},
	})
}
