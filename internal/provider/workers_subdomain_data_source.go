package provider

import (
	"context"
	"fmt"

	cloudflare "github.com/cloudflare/cloudflare-go/v7"
	"github.com/cloudflare/cloudflare-go/v7/workers"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type workersSubdomainDataSource struct {
	client *cloudflare.Client
}

var (
	_ datasource.DataSource              = (*workersSubdomainDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*workersSubdomainDataSource)(nil)
)

func NewWorkersSubdomainDataSource() datasource.DataSource {
	return &workersSubdomainDataSource{}
}

type workersSubdomainModel struct {
	AccountID types.String `tfsdk:"account_id"`
	Subdomain types.String `tfsdk:"subdomain"`
}

func (d *workersSubdomainDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workers_subdomain"
}

func (d *workersSubdomainDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "GET /accounts/{account_id}/workers/subdomain",
		Attributes: map[string]schema.Attribute{
			"account_id": schema.StringAttribute{
				Required:    true,
				Description: "Account identifier.",
			},
			"subdomain": schema.StringAttribute{
				Computed:    true,
				Description: "The workers.dev subdomain of the account (the `octocat` in `hello-world.octocat.workers.dev`).",
			},
		},
	}
}

func (d *workersSubdomainDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*cloudflare.Client)
	if !ok {
		resp.Diagnostics.AddError("unexpected provider data", fmt.Sprintf("expected *cloudflare.Client, got %T", req.ProviderData))
		return
	}
	d.client = client
}

func (d *workersSubdomainDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data workersSubdomainModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := d.client.Workers.Subdomains.Get(ctx, workers.SubdomainGetParams{
		AccountID: cloudflare.F(data.AccountID.ValueString()),
	})
	if err != nil {
		resp.Diagnostics.AddError("failed to fetch workers.dev subdomain", err.Error())
		return
	}

	data.Subdomain = types.StringValue(res.Subdomain)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
