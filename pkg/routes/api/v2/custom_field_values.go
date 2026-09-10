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
	"net/http"

	"code.vikunja.io/api/pkg/db"
	"code.vikunja.io/api/pkg/events"
	"code.vikunja.io/api/pkg/models"

	"github.com/danielgtaylor/huma/v2"
)

// RegisterCustomFieldValueRoutes wires the task custom-field value set/unset
// operations onto the Huma API. Values are task-scoped writes: a user with write
// permission on the task's project sets, replaces, and unsets them.
func RegisterCustomFieldValueRoutes(api huma.API) {
	tags := []string{"custom_field_values"}

	Register(api, huma.Operation{
		OperationID: "custom-field-values-set",
		Summary:     "Set a custom field value on a task",
		Description: "Sets the value of a custom field on a task, replacing any existing one. Idempotent: setting the same value twice has no further effect. The value must match the definition's immutable type, and the definition must belong to the task's project and not be archived. An empty multi-select value (option_ids: []) unsets the field. Requires write permission on the task.",
		Method:      http.MethodPut,
		Path:        "/tasks/{task}/custom-field-values/{definition}",
		Tags:        tags,
	}, customFieldValuesSet)

	Register(api, huma.Operation{
		OperationID: "custom-field-values-unset",
		Summary:     "Unset a custom field value on a task",
		Description: "Removes the custom field value from a task. Idempotent: unsetting a field that has no value is a no-op. Requires write permission on the task.",
		Method:      http.MethodDelete,
		Path:        "/tasks/{task}/custom-field-values/{definition}",
		Tags:        tags,
	}, customFieldValuesUnset)
}

func init() { AddRouteRegistrar(RegisterCustomFieldValueRoutes) }

// The set/unset operations are not standard CRUD on the value row: set is an
// upsert and unset is idempotent deletion. Both run manually in one session and
// call the model's permission helper instead of a generic handler.Do*.
func customFieldValuesSet(ctx context.Context, in *struct {
	TaskID       int64 `path:"task"`
	DefinitionID int64 `path:"definition"`
	Body         models.CustomFieldValue
}) (*singleBody[models.CustomFieldValue], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}

	s := db.NewSession()
	defer s.Close()

	can, err := models.CanWriteCustomFieldValue(s, in.TaskID, a)
	if err != nil {
		_ = s.Rollback()
		events.CleanupPending(s)
		return nil, translateDomainError(err)
	}
	if !can {
		_ = s.Rollback()
		events.CleanupPending(s)
		return nil, huma.Error403Forbidden("forbidden")
	}

	def, err := models.GetCustomFieldDefinitionByID(s, in.DefinitionID)
	if err != nil {
		_ = s.Rollback()
		events.CleanupPending(s)
		return nil, translateDomainError(err)
	}

	if err := models.SetCustomFieldValue(s, in.TaskID, def, &in.Body, a); err != nil {
		_ = s.Rollback()
		events.CleanupPending(s)
		return nil, translateDomainError(err)
	}

	// An empty multi-select value means "unset": the setter deleted the row, so
	// there is nothing to read back. Respond with the self-describing empty
	// selection instead of a 404 on the now-missing row.
	if in.Body.Type == models.CustomFieldTypeMultiSelect && in.Body.OptionIDs != nil && len(in.Body.OptionIDs) == 0 {
		if err := s.Commit(); err != nil {
			events.CleanupPending(s)
			return nil, translateDomainError(err)
		}
		events.DispatchPending(ctx, s)
		return &singleBody[models.CustomFieldValue]{Body: &models.CustomFieldValue{Type: models.CustomFieldTypeMultiSelect, OptionIDs: []int64{}}}, nil
	}

	// Read back what was stored so the response carries the canonical typed value.
	row, err := models.GetCustomFieldValue(s, in.TaskID, in.DefinitionID)
	if err != nil {
		_ = s.Rollback()
		events.CleanupPending(s)
		return nil, translateDomainError(err)
	}
	value, err := row.FromRow(s, def)
	if err != nil {
		_ = s.Rollback()
		events.CleanupPending(s)
		return nil, translateDomainError(err)
	}

	if err := s.Commit(); err != nil {
		events.CleanupPending(s)
		return nil, translateDomainError(err)
	}
	events.DispatchPending(ctx, s)
	return &singleBody[models.CustomFieldValue]{Body: value}, nil
}

func customFieldValuesUnset(ctx context.Context, in *struct {
	TaskID       int64 `path:"task"`
	DefinitionID int64 `path:"definition"`
}) (*emptyBody, error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}

	s := db.NewSession()
	defer s.Close()

	can, err := models.CanWriteCustomFieldValue(s, in.TaskID, a)
	if err != nil {
		_ = s.Rollback()
		events.CleanupPending(s)
		return nil, translateDomainError(err)
	}
	if !can {
		_ = s.Rollback()
		events.CleanupPending(s)
		return nil, huma.Error403Forbidden("forbidden")
	}

	if err := models.UnsetCustomFieldValue(s, in.TaskID, in.DefinitionID, a); err != nil {
		_ = s.Rollback()
		events.CleanupPending(s)
		return nil, translateDomainError(err)
	}

	if err := s.Commit(); err != nil {
		events.CleanupPending(s)
		return nil, translateDomainError(err)
	}
	events.DispatchPending(ctx, s)
	return &emptyBody{}, nil
}
