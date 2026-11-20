package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/kcmhub/terraform-provider-kcm/internal/client"
)

// kindResource is a KCM object declared as code: one Terraform resource type per kind, driven by its fields.
type kindResource struct {
	typeName    string
	kind        string
	description string
	fields      []field
	data        *providerData
}

var (
	_ resource.Resource                = (*kindResource)(nil)
	_ resource.ResourceWithConfigure   = (*kindResource)(nil)
	_ resource.ResourceWithImportState = (*kindResource)(nil)
)

func (r *kindResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + r.typeName
}

func (r *kindResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attributes := map[string]schema.Attribute{
		"id": schema.StringAttribute{
			Computed:      true,
			Description:   "Kind and name of the object (`" + r.kind + "/<name>`).",
			PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		},
		"name": schema.StringAttribute{
			Required: true,
			Description: "Name of the object within its kind in KCM as code (`metadata.name`): letters, digits and " +
				"`._@:-`. Changing it replaces the object.",
			PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
		},
		"managed_by": schema.StringAttribute{
			Computed:      true,
			Description:   "Source that manages the object in KCM (`iac:<source>`).",
			PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		},
	}
	for _, f := range r.fields {
		attributes[f.attr] = schemaAttribute(f)
	}
	resp.Schema = schema.Schema{Description: r.description, Attributes: attributes}
}

// schemaAttribute: a field declared is managed; a field left out keeps the value KCM has (optional and computed).
func schemaAttribute(f field) schema.Attribute {
	optional := !f.required
	computed := !f.required && f.kind != jsonField && f.kind != secretFileField
	switch f.kind {
	case boolField:
		modifiers := []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()}
		if f.forceNew {
			modifiers = append(modifiers, boolplanmodifier.RequiresReplace())
		}
		return schema.BoolAttribute{Required: f.required, Optional: optional, Computed: computed, Description: f.description, PlanModifiers: modifiers}
	case intField:
		modifiers := []planmodifier.Int64{int64planmodifier.UseStateForUnknown()}
		if f.forceNew {
			modifiers = append(modifiers, int64planmodifier.RequiresReplace())
		}
		return schema.Int64Attribute{Required: f.required, Optional: optional, Computed: computed, Description: f.description, PlanModifiers: modifiers}
	case stringListField:
		return schema.ListAttribute{ElementType: types.StringType, Required: f.required, Optional: optional, Computed: computed,
			Description: f.description, PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()}}
	case stringSetField:
		return schema.SetAttribute{ElementType: types.StringType, Required: f.required, Optional: optional, Computed: computed,
			Description: f.description, PlanModifiers: []planmodifier.Set{setplanmodifier.UseStateForUnknown()}}
	case stringMapField:
		return schema.MapAttribute{ElementType: types.StringType, Required: f.required, Optional: optional, Computed: computed,
			Description: f.description, PlanModifiers: []planmodifier.Map{mapplanmodifier.UseStateForUnknown()}}
	default:
		modifiers := []planmodifier.String{}
		if computed {
			modifiers = append(modifiers, stringplanmodifier.UseStateForUnknown())
		}
		if f.forceNew && f.scopePath {
			// the same path written with another percent-encoding (after an import) is updated in place
			modifiers = append(modifiers, stringplanmodifier.RequiresReplaceIf(differentScopePath,
				"Replaced when the path names other objects.", "Replaced when the path names other objects."))
		} else if f.forceNew {
			modifiers = append(modifiers, stringplanmodifier.RequiresReplace())
		}
		return schema.StringAttribute{Required: f.required, Optional: optional, Computed: computed, Description: f.description, PlanModifiers: modifiers}
	}
}

func (r *kindResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	data, ok := req.ProviderData.(*providerData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("got %T", req.ProviderData))
		return
	}
	r.data = data
}

func (r *kindResource) hasJSON() bool {
	for _, f := range r.fields {
		if f.kind == jsonField {
			return true
		}
	}
	return false
}

// document builds the document of the object from a plan or a state.
func (r *kindResource) document(ctx context.Context, from getter) (*client.Document, diag.Diagnostics) {
	var name types.String
	diags := from.GetAttribute(ctx, path.Root("name"), &name)
	spec, d := toSpec(ctx, r.fields, from)
	diags.Append(d...)
	return &client.Document{APIVersion: client.APIVersion, Kind: r.kind, Metadata: client.Metadata{Name: name.ValueString()}, Spec: spec}, diags
}

func planErrors(plan *client.Plan) string {
	if plan == nil || len(plan.Errors) == 0 {
		return ""
	}
	return strings.Join(plan.Errors, "\n")
}

func (r *kindResource) apply(ctx context.Context, from getter, diags *diag.Diagnostics) *client.Document {
	doc, d := r.document(ctx, from)
	diags.Append(d...)
	if diags.HasError() {
		return nil
	}
	plan, err := r.data.client.Put(ctx, doc, r.data.allowDestroy, false)
	if err != nil {
		diags.AddError("KCM refused "+r.kind+" "+doc.Metadata.Name, err.Error())
		return nil
	}
	if !plan.Applied {
		diags.AddError("KCM did not apply "+r.kind+" "+doc.Metadata.Name, planErrors(plan))
		return nil
	}
	return doc
}

func (r *kindResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	doc := r.apply(ctx, req.Plan, &resp.Diagnostics)
	if doc == nil {
		return
	}
	resp.State.Raw = req.Plan.Raw
	r.refresh(ctx, doc.Metadata.Name, req.Plan, resp.State.SetAttribute, &resp.Diagnostics)
}

func (r *kindResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	doc := r.apply(ctx, req.Plan, &resp.Diagnostics)
	if doc == nil {
		return
	}
	resp.State.Raw = req.Plan.Raw
	r.refresh(ctx, doc.Metadata.Name, req.Plan, resp.State.SetAttribute, &resp.Diagnostics)
}

type setFunc func(ctx context.Context, p path.Path, val interface{}) diag.Diagnostics

func (f setFunc) SetAttribute(ctx context.Context, p path.Path, val interface{}) diag.Diagnostics {
	return f(ctx, p, val)
}

// refresh completes the state after an apply: the values left out of the configuration come from KCM, the declared
// ones stay as written (KCM may order or format them differently).
func (r *kindResource) refresh(ctx context.Context, name string, plan getter, set setFunc, diags *diag.Diagnostics) {
	current, err := r.data.client.Get(ctx, r.kind, name)
	if err != nil {
		diags.AddError("Reading "+r.kind+" "+name+" from KCM", err.Error())
		return
	}
	diags.Append(set(ctx, path.Root("id"), types.StringValue(r.kind+"/"+name))...)
	diags.Append(set(ctx, path.Root("managed_by"), types.StringValue(current.Metadata.ManagedBy))...)
	var computed []field
	for _, f := range r.fields {
		if unknown(ctx, f, plan) {
			computed = append(computed, f)
		}
	}
	diags.Append(fromSpec(ctx, computed, current.Spec, set)...)
}

func (r *kindResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var name types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("name"), &name)...)
	if resp.Diagnostics.HasError() {
		return
	}
	current, err := r.data.client.Get(ctx, r.kind, name.ValueString())
	if client.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if client.IsUnreachable(err) {
		// keep the state: the apply can still fix what makes Kafka unreachable (the cluster, a secret)
		resp.Diagnostics.AddWarning("Kafka unreachable for "+r.kind+" "+name.ValueString(),
			err.Error()+" The state is kept as it was.")
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Reading "+r.kind+" "+name.ValueString()+" from KCM", err.Error())
		return
	}
	// A plan of KCM for the object as it is in the state tells the drift that the spec read back cannot show: a JSON
	// field changed in KCM, or the content of a secret file changed (same file name, new value)
	var changedSecrets []field
	if declared := r.declaredSecrets(ctx, req.State); r.hasJSON() || len(declared) > 0 {
		doc, d := r.document(ctx, req.State)
		resp.Diagnostics.Append(d...)
		plan, err := r.data.client.PlanDocuments(ctx, []*client.Document{doc})
		if err != nil {
			resp.Diagnostics.AddError("Planning "+r.kind+" "+name.ValueString()+" in KCM", err.Error())
			return
		}
		if r.hasJSON() && plan.HasChanges() {
			for _, f := range r.fields {
				if f.kind == jsonField {
					resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(f.attr), types.StringValue(jsonOf(r.fields, current.Spec)))...)
				}
			}
		}
		changedSecrets = changedSecretFiles(plan, r.kind, name.ValueString(), declared)
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.StringValue(r.kind+"/"+name.ValueString()))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("managed_by"), types.StringValue(current.Metadata.ManagedBy))...)
	r.readSecrets(ctx, req.State, current.Spec)
	keepScopePaths(ctx, r.fields, req.State, current.Spec)
	resp.Diagnostics.Append(fromSpec(ctx, r.fields, current.Spec, &resp.State)...)
	for _, f := range changedSecrets {
		// null in the state: the next plan updates the object, which sends the new content
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(f.attr), types.StringNull())...)
		resp.Diagnostics.AddWarning("Secret of "+r.kind+" "+name.ValueString()+" changed",
			"The content of the secret file set in "+f.attr+" is not the one KCM holds: the next apply sends it.")
	}
}

// declaredSecrets returns the secret file attributes set in a state.
func (r *kindResource) declaredSecrets(ctx context.Context, state getter) []field {
	var declared []field
	for _, f := range r.fields {
		if f.kind != secretFileField {
			continue
		}
		var file types.String
		state.GetAttribute(ctx, path.Root(f.attr), &file)
		if !file.IsNull() && !file.IsUnknown() && file.ValueString() != "" {
			declared = append(declared, f)
		}
	}
	return declared
}

// readSecrets: a secret file reference read back from KCM is the one applied; when KCM has none (never applied
// through KCM as code), the state keeps the declared one so that the next apply sets it.
func (r *kindResource) readSecrets(ctx context.Context, state getter, spec map[string]interface{}) {
	for _, f := range r.fields {
		if f.kind != secretFileField {
			continue
		}
		if _, ok := getPath(spec, f.path); ok {
			continue
		}
		var declared types.String
		state.GetAttribute(ctx, path.Root(f.attr), &declared)
		if !declared.IsNull() && declared.ValueString() != "" {
			setPath(spec, f.path, map[string]interface{}{"file": declared.ValueString()})
		}
	}
}

func differentScopePath(_ context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
	resp.RequiresReplace = !sameScopePath(req.StateValue.ValueString(), req.PlanValue.ValueString())
}

// keepScopePaths: a KCM path read back with another percent-encoding than the declared one keeps the declared one.
func keepScopePaths(ctx context.Context, fields []field, state getter, spec map[string]interface{}) {
	for _, f := range fields {
		if !f.scopePath {
			continue
		}
		read, ok := getPath(spec, f.path)
		if !ok {
			continue
		}
		var declared types.String
		state.GetAttribute(ctx, path.Root(f.attr), &declared)
		if !declared.IsNull() && !declared.IsUnknown() && sameScopePath(declared.ValueString(), text(read)) {
			setPath(spec, f.path, declared.ValueString())
		}
	}
}

func (r *kindResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var name types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("name"), &name)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan, err := r.data.client.Delete(ctx, r.kind, name.ValueString(), r.data.allowDestroy)
	if client.IsNotFound(err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("KCM refused to remove "+r.kind+" "+name.ValueString(), err.Error())
		return
	}
	if !plan.Applied {
		resp.Diagnostics.AddError("KCM did not remove "+r.kind+" "+name.ValueString(), planErrors(plan))
	}
}

// ImportState imports an object by its name in KCM as code: its declared name when it is managed, its export name
// otherwise (GET /api/v1/iac/export lists them). The next apply adopts it.
func (r *kindResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	name := strings.TrimPrefix(req.ID, r.kind+"/")
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), types.StringValue(name))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.StringValue(r.kind+"/"+name))...)
}
