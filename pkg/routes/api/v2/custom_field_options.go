// Vikunja is a to-do list application to facilitate your life.
// Copyright 2018-present Vikunja and contributors. All rights reserved.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package apiv2

import (
	"context"
	"fmt"
	"net/http"

	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/web/handler"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/conditional"
)

type customFieldOptionListBody struct {
	Body Paginated[*models.CustomFieldOption]
}

// RegisterCustomFieldOptionRoutes wires the select-option CRUD of custom field
// definitions onto the Huma API. Options nest under a definition, which nests
// under a project; project administrators manage them.
func RegisterCustomFieldOptionRoutes(api huma.API) {
	tags := []string{"custom_field_options"}

	Register(api, huma.Operation{
		OperationID: "custom-field-options-list",
		Summary:     "List the options of a custom field definition",
		Description: "Returns the select options of a custom field definition. Archived options are hidden unless include_archived=true; they continue to describe retained values.",
		Method:      http.MethodGet,
		Path:        "/projects/{project}/custom-field-definitions/{definition}/options",
		Tags:        tags,
	}, customFieldOptionsList)

	Register(api, huma.Operation{
		OperationID: "custom-field-options-create",
		Summary:     "Create a custom field option",
		Description: "Creates a select option for the definition in the path. Only project administrators may do this, and only single-select and multi-select definitions may have options. The machine key is immutable once set.",
		Method:      http.MethodPost,
		Path:        "/projects/{project}/custom-field-definitions/{definition}/options",
		Tags:        tags,
	}, customFieldOptionsCreate)

	Register(api, huma.Operation{
		OperationID: "custom-field-options-read",
		Summary:     "Get a custom field option",
		Description: "Returns one select option. It must belong to the definition and project in the path. Sends an ETag; pass it as If-None-Match on a later read to get a 304 Not Modified.",
		Method:      http.MethodGet,
		Path:        "/projects/{project}/custom-field-definitions/{definition}/options/{option}",
		Tags:        tags,
	}, customFieldOptionsRead)

	Register(api, huma.Operation{
		OperationID: "custom-field-options-update",
		Summary:     "Update a custom field option",
		Description: "Replaces a select option's editable fields. Only project administrators may update it; the machine key and its definition cannot change. Use PATCH for a partial update.",
		Method:      http.MethodPut,
		Path:        "/projects/{project}/custom-field-definitions/{definition}/options/{option}",
		Tags:        tags,
	}, customFieldOptionsUpdate)

	Register(api, huma.Operation{
		OperationID: "custom-field-options-delete",
		Summary:     "Archive a custom field option",
		Description: "Archives the option rather than removing it, so it can no longer be selected but continues to describe retained values. Idempotent. Only project administrators may archive options.",
		Method:      http.MethodDelete,
		Path:        "/projects/{project}/custom-field-definitions/{definition}/options/{option}",
		Tags:        tags,
	}, customFieldOptionsDelete)
}

func init() { AddRouteRegistrar(RegisterCustomFieldOptionRoutes) }

func customFieldOptionsList(ctx context.Context, in *struct {
	ProjectID       int64 `path:"project"`
	DefinitionID    int64 `path:"definition"`
	IncludeArchived bool  `query:"include_archived"`
	ListParams
}) (*customFieldOptionListBody, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	result, _, total, err := handler.DoReadAll(ctx, &models.CustomFieldOption{DefinitionID: in.DefinitionID, ProjectID: in.ProjectID, IncludeArchived: in.IncludeArchived}, a, in.Q, in.Page, in.PerPage)
	if err != nil {
		return nil, translateDomainError(err)
	}
	items, ok := result.([]*models.CustomFieldOption)
	if !ok {
		return nil, fmt.Errorf("customFieldOptions.ReadAll returned unexpected type %T (expected []*models.CustomFieldOption)", result)
	}
	return &customFieldOptionListBody{Body: NewPaginated(items, total, in.Page, in.PerPage)}, nil
}

type customFieldOptionReadBody struct {
	models.CustomFieldOption
	MaxPermission models.Permission `json:"max_permission" readOnly:"true" doc:"The maximum permission the requesting user has on this option (0=read, 1=read/write, 2=admin)."`
}

func customFieldOptionsRead(ctx context.Context, in *struct {
	ProjectID    int64 `path:"project"`
	DefinitionID int64 `path:"definition"`
	OptionID     int64 `path:"option"`
	conditional.Params
}) (*singleReadBody[customFieldOptionReadBody], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	opt := &models.CustomFieldOption{ID: in.OptionID, DefinitionID: in.DefinitionID, ProjectID: in.ProjectID}
	maxPermission, err := handler.DoReadOne(ctx, opt, a)
	if err != nil {
		return nil, translateDomainError(err)
	}
	body := &customFieldOptionReadBody{CustomFieldOption: *opt, MaxPermission: models.Permission(maxPermission)}
	return conditionalReadResponse(&in.Params, body, opt.Updated, maxPermission)
}

func customFieldOptionsCreate(ctx context.Context, in *struct {
	ProjectID    int64 `path:"project"`
	DefinitionID int64 `path:"definition"`
	Body         models.CustomFieldOption
}) (*singleBody[models.CustomFieldOption], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	in.Body.DefinitionID = in.DefinitionID
	in.Body.ProjectID = in.ProjectID // URL wins over body
	if err := handler.DoCreate(ctx, &in.Body, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.CustomFieldOption]{Body: &in.Body}, nil
}

// Body matches the read shape so AutoPatch's GET→PUT echo of max_permission validates.
func customFieldOptionsUpdate(ctx context.Context, in *struct {
	ProjectID    int64 `path:"project"`
	DefinitionID int64 `path:"definition"`
	OptionID     int64 `path:"option"`
	Body         customFieldOptionReadBody
}) (*singleBody[models.CustomFieldOption], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	opt := &in.Body.CustomFieldOption
	opt.ID = in.OptionID
	opt.DefinitionID = in.DefinitionID // parents from the path scope the update
	opt.ProjectID = in.ProjectID
	if err := handler.DoUpdate(ctx, opt, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.CustomFieldOption]{Body: opt}, nil
}

func customFieldOptionsDelete(ctx context.Context, in *struct {
	ProjectID    int64 `path:"project"`
	DefinitionID int64 `path:"definition"`
	OptionID     int64 `path:"option"`
}) (*emptyBody, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	if err := handler.DoDelete(ctx, &models.CustomFieldOption{ID: in.OptionID, DefinitionID: in.DefinitionID, ProjectID: in.ProjectID}, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &emptyBody{}, nil
}
