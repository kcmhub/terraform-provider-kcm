package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kcmhub/terraform-provider-kcm/internal/client"
)

// fieldType is how a Terraform attribute maps to the spec of a document.
type fieldType int

const (
	stringField fieldType = iota
	boolField
	intField
	stringListField
	// stringSetField is a list in the spec whose order does not matter (members, operations, tags).
	stringSetField
	stringMapField
	// secretFileField is the name of a file of KCM_IAC_SECRETS_DIR: the spec holds {file: name}.
	secretFileField
	// jsonField is a JSON object merged into the spec (the fields of TopicMetadata).
	jsonField
)

// field is one attribute of a resource and its place in the spec.
type field struct {
	attr     string
	path     []string
	kind     fieldType
	required bool
	forceNew bool
	// scopePath: a KCM path (/groups/{group}/...) whose names may be percent-encoded in any way; KCM reads it back
	// with its own encoding, so an equivalent value is not a change.
	scopePath bool
	// ref: a reference to another object (the cluster or the environment of the object). It has an alternative
	// (refAlt, the attribute that references the same object by uid, or by names): exactly one of the two is set.
	// A reference is never computed: a value KCM gave back would clash with the alternative.
	ref bool
	// uidRef: the reference is a uid. KCM does not write uids in the spec it gives back, so it is never read back.
	uidRef bool
	// refAlt: the attribute that references the same object the other way.
	refAlt      string
	description string
}

// getter reads an attribute of a plan, a state or a config.
type getter interface {
	GetAttribute(ctx context.Context, p path.Path, target interface{}) diag.Diagnostics
}

// setter writes an attribute of a state.
type setter interface {
	SetAttribute(ctx context.Context, p path.Path, val interface{}) diag.Diagnostics
}

func setPath(spec map[string]interface{}, keys []string, value interface{}) {
	node := spec
	for _, k := range keys[:len(keys)-1] {
		child, ok := node[k].(map[string]interface{})
		if !ok {
			child = map[string]interface{}{}
			node[k] = child
		}
		node = child
	}
	node[keys[len(keys)-1]] = value
}

func getPath(spec map[string]interface{}, keys []string) (interface{}, bool) {
	var node interface{} = spec
	for _, k := range keys {
		m, ok := node.(map[string]interface{})
		if !ok {
			return nil, false
		}
		node, ok = m[k]
		if !ok {
			return nil, false
		}
	}
	return node, node != nil
}

// toSpec builds the spec of a document from the known, non null attributes.
func toSpec(ctx context.Context, fields []field, from getter) (map[string]interface{}, diag.Diagnostics) {
	spec := map[string]interface{}{}
	var diags diag.Diagnostics
	for _, f := range fields {
		p := path.Root(f.attr)
		switch f.kind {
		case stringField, secretFileField:
			var v types.String
			diags.Append(from.GetAttribute(ctx, p, &v)...)
			if v.IsNull() || v.IsUnknown() {
				continue
			}
			if f.kind == secretFileField {
				setPath(spec, f.path, map[string]interface{}{"file": v.ValueString()})
			} else {
				setPath(spec, f.path, v.ValueString())
			}
		case boolField:
			var v types.Bool
			diags.Append(from.GetAttribute(ctx, p, &v)...)
			if !v.IsNull() && !v.IsUnknown() {
				setPath(spec, f.path, v.ValueBool())
			}
		case intField:
			var v types.Int64
			diags.Append(from.GetAttribute(ctx, p, &v)...)
			if !v.IsNull() && !v.IsUnknown() {
				setPath(spec, f.path, v.ValueInt64())
			}
		case stringListField:
			var v types.List
			diags.Append(from.GetAttribute(ctx, p, &v)...)
			if v.IsNull() || v.IsUnknown() {
				continue
			}
			var items []string
			diags.Append(v.ElementsAs(ctx, &items, false)...)
			setPath(spec, f.path, items)
		case stringSetField:
			var v types.Set
			diags.Append(from.GetAttribute(ctx, p, &v)...)
			if v.IsNull() || v.IsUnknown() {
				continue
			}
			var items []string
			diags.Append(v.ElementsAs(ctx, &items, false)...)
			sort.Strings(items)
			setPath(spec, f.path, items)
		case stringMapField:
			var v types.Map
			diags.Append(from.GetAttribute(ctx, p, &v)...)
			if v.IsNull() || v.IsUnknown() {
				continue
			}
			items := map[string]string{}
			diags.Append(v.ElementsAs(ctx, &items, false)...)
			setPath(spec, f.path, items)
		case jsonField:
			var v types.String
			diags.Append(from.GetAttribute(ctx, p, &v)...)
			if v.IsNull() || v.IsUnknown() || v.ValueString() == "" {
				continue
			}
			var object map[string]interface{}
			if err := json.Unmarshal([]byte(v.ValueString()), &object); err != nil {
				diags.AddAttributeError(p, "Invalid JSON", "a JSON object is expected: "+err.Error())
				continue
			}
			for k, value := range object {
				spec[k] = value
			}
		}
	}
	return spec, diags
}

// readable returns the fields to read back from KCM into a state. The references are special: a uid is never read back
// (KCM does not put it in the spec), and a reference by names is only read back when the state has no reference by uid
// (the object was imported, or declared by names): otherwise the value KCM gives would turn into a difference with a
// configuration that references the object by uid.
func readable(ctx context.Context, fields []field, state getter) []field {
	set := func(attr string) bool {
		var v types.String
		if state.GetAttribute(ctx, path.Root(attr), &v).HasError() {
			return false
		}
		return !v.IsNull() && !v.IsUnknown() && v.ValueString() != ""
	}
	var kept []field
	for _, f := range fields {
		if f.uidRef {
			continue
		}
		if f.ref && f.refAlt != "" && set(f.refAlt) {
			continue
		}
		kept = append(kept, f)
	}
	return kept
}

// unknown tells whether the attribute is unknown in a plan (left out of the configuration: KCM gives its value).
func unknown(ctx context.Context, f field, from getter) bool {
	p := path.Root(f.attr)
	switch f.kind {
	case boolField:
		var v types.Bool
		from.GetAttribute(ctx, p, &v)
		return v.IsUnknown()
	case intField:
		var v types.Int64
		from.GetAttribute(ctx, p, &v)
		return v.IsUnknown()
	case stringListField:
		var v types.List
		from.GetAttribute(ctx, p, &v)
		return v.IsUnknown()
	case stringSetField:
		var v types.Set
		from.GetAttribute(ctx, p, &v)
		return v.IsUnknown()
	case stringMapField:
		var v types.Map
		from.GetAttribute(ctx, p, &v)
		return v.IsUnknown()
	default:
		var v types.String
		from.GetAttribute(ctx, p, &v)
		return v.IsUnknown()
	}
}

// fromSpec writes the attributes read from a spec; an attribute absent from the spec becomes null. The JSON fields
// are left as they are: their drift is detected by a plan of KCM (see the resource).
func fromSpec(ctx context.Context, fields []field, spec map[string]interface{}, to setter) diag.Diagnostics {
	var diags diag.Diagnostics
	for _, f := range fields {
		p := path.Root(f.attr)
		value, ok := getPath(spec, f.path)
		switch f.kind {
		case stringField:
			if ok {
				diags.Append(to.SetAttribute(ctx, p, types.StringValue(text(value)))...)
			} else {
				diags.Append(to.SetAttribute(ctx, p, types.StringNull())...)
			}
		case secretFileField:
			file := ""
			if m, isMap := value.(map[string]interface{}); ok && isMap {
				file = text(m["file"])
			}
			if file != "" {
				diags.Append(to.SetAttribute(ctx, p, types.StringValue(file))...)
			} else {
				diags.Append(to.SetAttribute(ctx, p, types.StringNull())...)
			}
		case boolField:
			if b, isBool := value.(bool); ok && isBool {
				diags.Append(to.SetAttribute(ctx, p, types.BoolValue(b))...)
			} else {
				diags.Append(to.SetAttribute(ctx, p, types.BoolNull())...)
			}
		case intField:
			if n, isNumber := value.(float64); ok && isNumber {
				diags.Append(to.SetAttribute(ctx, p, types.Int64Value(int64(n)))...)
			} else {
				diags.Append(to.SetAttribute(ctx, p, types.Int64Null())...)
			}
		case stringListField:
			if items, isList := value.([]interface{}); ok && isList {
				values := make([]attr.Value, 0, len(items))
				for _, item := range items {
					values = append(values, types.StringValue(text(item)))
				}
				list, d := types.ListValue(types.StringType, values)
				diags.Append(d...)
				diags.Append(to.SetAttribute(ctx, p, list)...)
			} else {
				diags.Append(to.SetAttribute(ctx, p, types.ListNull(types.StringType))...)
			}
		case stringSetField:
			if items, isList := value.([]interface{}); ok && isList {
				values := make([]attr.Value, 0, len(items))
				for _, item := range items {
					values = append(values, types.StringValue(text(item)))
				}
				set, d := types.SetValue(types.StringType, values)
				diags.Append(d...)
				diags.Append(to.SetAttribute(ctx, p, set)...)
			} else {
				diags.Append(to.SetAttribute(ctx, p, types.SetNull(types.StringType))...)
			}
		case stringMapField:
			if items, isMap := value.(map[string]interface{}); ok && isMap {
				values := map[string]attr.Value{}
				for k, item := range items {
					values[k] = types.StringValue(text(item))
				}
				m, d := types.MapValue(types.StringType, values)
				diags.Append(d...)
				diags.Append(to.SetAttribute(ctx, p, m)...)
			} else {
				diags.Append(to.SetAttribute(ctx, p, types.MapNull(types.StringType))...)
			}
		}
	}
	return diags
}

// jsonOf returns the spec without the keys of the other fields, as compact JSON with sorted keys.
func jsonOf(fields []field, spec map[string]interface{}) string {
	others := map[string]bool{}
	for _, f := range fields {
		if f.kind != jsonField {
			others[f.path[0]] = true
		}
	}
	keys := make([]string, 0, len(spec))
	for k := range spec {
		if !others[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	object := map[string]interface{}{}
	for _, k := range keys {
		object[k] = spec[k]
	}
	data, _ := json.Marshal(object) // encoding/json sorts the keys of maps
	return string(data)
}

func text(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}

// sameScopePath tells whether two KCM paths name the same objects once their percent-encoding is decoded.
func sameScopePath(a, b string) bool {
	if a == b {
		return true
	}
	da, errA := url.PathUnescape(a)
	db, errB := url.PathUnescape(b)
	return errA == nil && errB == nil && da == db
}

// secretDiffField is the field of a plan diff for a secret file attribute: sasl.passwordFrom is sasl.password.
func secretDiffField(f field) string {
	return strings.TrimSuffix(strings.Join(f.path, "."), "From")
}

// changedSecretFiles returns the declared secret file attributes whose secret KCM plans to change.
func changedSecretFiles(plan *client.Plan, kind, name string, declared []field) []field {
	var changed []field
	if plan == nil {
		return changed
	}
	for _, c := range plan.Changes {
		if c.Kind != kind || c.Name != name || c.Action != "UPDATE" {
			continue
		}
		for _, f := range declared {
			for _, d := range c.Diff {
				if d.Field == secretDiffField(f) {
					changed = append(changed, f)
					break
				}
			}
		}
	}
	return changed
}
