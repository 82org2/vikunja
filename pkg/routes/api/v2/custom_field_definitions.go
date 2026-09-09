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

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/web/handler"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/conditional"
)

type customFieldDefinitionListBody struct {
	Body Paginated[*models.CustomFieldDefinition]
}

// RegisterCustomFieldDefinitionRoutes wires the project-scoped custom field
// definition CRUD onto the Huma API. Definitions are nested under the project:
// readers can list them, project administrators create, edit, archive, and
// permanently delete them.
func RegisterCustomFieldDefinitionRoutes(api huma.API) {
	tags := []string{"custom_field_definitions"}

	Register(api, huma.Operation{
		OperationID: "custom-field-definitions-list",
		Summary:     "List the custom field definitions of a project",
		Description: "Returns the custom field definitions of the given project that the authenticated user can read. Archived definitions are hidden unless include_archived=true; they are still returned when describing retained values.",
		Method:      http.MethodGet,
		Path:        "/projects/{project}/custom-field-definitions",
		Tags:        tags,
	}, customFieldDefinitionsList)

	Register(api, huma.Operation{
		OperationID: "custom-field-definitions-create",
		Summary:     "Create a custom field definition",
		Description: "Creates a custom field definition in the given project. The project is taken from the URL, and only project administrators may create definitions. The machine key and field type are immutable once set.",
		Method:      http.MethodPost,
		Path:        "/projects/{project}/custom-field-definitions",
		Tags:        tags,
	}, customFieldDefinitionsCreate)

	Register(api, huma.Operation{
		OperationID: "custom-field-definitions-read",
		Summary:     "Get a custom field definition",
		Description: "Returns one custom field definition. It must belong to the project in the path. Sends an ETag; pass it as If-None-Match on a later read to get a 304 Not Modified.",
		Method:      http.MethodGet,
		Path:        "/projects/{project}/custom-field-definitions/{definition}",
		Tags:        tags,
	}, customFieldDefinitionsRead)

	Register(api, huma.Operation{
		OperationID: "custom-field-definitions-update",
		Summary:     "Update a custom field definition",
		Description: "Replaces a custom field definition's editable fields. The definition must belong to the project in the path, and only project administrators may update it. The machine key, field type, and project cannot be changed. Use PATCH for a partial update.",
		Method:      http.MethodPut,
		Path:        "/projects/{project}/custom-field-definitions/{definition}",
		Tags:        tags,
	}, customFieldDefinitionsUpdate)

	Register(api, huma.Operation{
		OperationID: "custom-field-definitions-delete",
		Summary:     "Archive a custom field definition",
		Description: "Archives the definition rather than removing it: retained values and options stay visible and describable, and archived definitions cannot receive new values. Idempotent. Only project administrators may archive. For actual removal use the permanent-delete action.",
		Method:      http.MethodDelete,
		Path:        "/projects/{project}/custom-field-definitions/{definition}",
		Tags:        tags,
	}, customFieldDefinitionsDelete)

	Register(api, huma.Operation{
		OperationID:   "custom-field-definitions-permanent-delete",
		Summary:       "Permanently delete a custom field definition",
		Description:   "Removes the definition together with its values, options, and multi-select memberships. Only project administrators may do this. A definition that still has values returns a conflict unless delete_values=true is supplied; the confirmed operation is atomic.",
		Method:        http.MethodPost,
		Path:          "/projects/{project}/custom-field-definitions/{definition}/permanent-delete",
		DefaultStatus: http.StatusNoContent,
		Tags:          tags,
	}, customFieldDefinitionsPermanentDelete)
}

func init() { AddRouteRegistrar(RegisterCustomFieldDefinitionRoutes) }

func customFieldDefinitionsList(ctx context.Context, in *struct {
	ProjectID       int64 `path:"project"`
	IncludeArchived bool  `query:"include_archived"`
	ListParams
}) (*customFieldDefinitionListBody, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	result, _, total, err := handler.DoReadAll(ctx, &models.CustomFieldDefinition{ProjectID: in.ProjectID, IncludeArchived: in.IncludeArchived}, a, in.Q, in.Page, in.PerPage)
	if err != nil {
		return nil, translateDomainError(err)
	}
	items, ok := result.([]*models.CustomFieldDefinition)
	if !ok {
		return nil, fmt.Errorf("customFieldDefinitions.ReadAll returned unexpected type %T (expected []*models.CustomFieldDefinition)", result)
	}
	return &customFieldDefinitionListBody{Body: NewPaginated(items, total, in.Page, in.PerPage)}, nil
}

type customFieldDefinitionReadBody struct {
	models.CustomFieldDefinition
	MaxPermission models.Permission `json:"max_permission" readOnly:"true" doc:"The maximum permission the requesting user has on this definition (0=read, 1=read/write, 2=admin)."`
}

func customFieldDefinitionsRead(ctx context.Context, in *struct {
	ProjectID    int64 `path:"project"`
	DefinitionID int64 `path:"definition"`
	conditional.Params
}) (*singleReadBody[customFieldDefinitionReadBody], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	def := &models.CustomFieldDefinition{ID: in.DefinitionID, ProjectID: in.ProjectID}
	maxPermission, err := handler.DoReadOne(ctx, def, a)
	if err != nil {
		return nil, translateDomainError(err)
	}
	body := &customFieldDefinitionReadBody{CustomFieldDefinition: *def, MaxPermission: models.Permission(maxPermission)}
	return conditionalReadResponse(&in.Params, body, def.Updated, maxPermission)
}

func customFieldDefinitionsCreate(ctx context.Context, in *struct {
	ProjectID int64 `path:"project"`
	Body      models.CustomFieldDefinition
}) (*singleBody[models.CustomFieldDefinition], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	in.Body.ProjectID = in.ProjectID // URL wins over body
	if err := handler.DoCreate(ctx, &in.Body, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.CustomFieldDefinition]{Body: &in.Body}, nil
}

// Body matches the read shape so AutoPatch's GET→PUT echo of max_permission validates.
func customFieldDefinitionsUpdate(ctx context.Context, in *struct {
	ProjectID    int64 `path:"project"`
	DefinitionID int64 `path:"definition"`
	Body         customFieldDefinitionReadBody
}) (*singleBody[models.CustomFieldDefinition], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	def := &in.Body.CustomFieldDefinition
	def.ID = in.DefinitionID
	def.ProjectID = in.ProjectID // parent from the path scopes the update
	if err := handler.DoUpdate(ctx, def, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.CustomFieldDefinition]{Body: def}, nil
}

func customFieldDefinitionsDelete(ctx context.Context, in *struct {
	ProjectID    int64 `path:"project"`
	DefinitionID int64 `path:"definition"`
}) (*emptyBody, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	if err := handler.DoDelete(ctx, &models.CustomFieldDefinition{ID: in.DefinitionID, ProjectID: in.ProjectID}, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &emptyBody{}, nil
}

// customFieldDefinitionsPermanentDelete is a non-CRUD destructive action, so the
// permission gate lives in the handler rather than a generic Do*: the definition
// must be admin-manageable, and DeletePermanently enforces the values-conflict.
func customFieldDefinitionsPermanentDelete(ctx context.Context, in *struct {
	ProjectID    int64 `path:"project"`
	DefinitionID int64 `path:"definition"`
	Body         struct {
		DeleteValues bool `json:"delete_values" doc:"Delete values already stored on this definition as part of the operation. Permanently deleting a populated definition without this flag returns a conflict."`
	}
}) (*emptyBody, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}

	s := db.NewSession()
	defer s.Close()

	def := &models.CustomFieldDefinition{ID: in.DefinitionID, ProjectID: in.ProjectID}
	can, err := def.CanDelete(s, a)
	if err != nil {
		_ = s.Rollback()
		return nil, translateDomainError(err)
	}
	if !can {
		_ = s.Rollback()
		return nil, huma.Error403Forbidden("forbidden")
	}

	if err := def.DeletePermanently(s, in.Body.DeleteValues); err != nil {
		_ = s.Rollback()
		return nil, translateDomainError(err)
	}
	if err := s.Commit(); err != nil {
		return nil, translateDomainError(err)
	}
	return &emptyBody{}, nil
}
