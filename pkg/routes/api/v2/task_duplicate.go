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

	"code.vikunja.io/api/pkg/models"
	"code.vikunja.io/api/pkg/web/handler"

	"github.com/danielgtaylor/huma/v2"
)

// RegisterTaskDuplicateRoutes wires the task-duplicate action onto the Huma API.
//
// TaskDuplicate is a CRUDable Create, so the handler reuses handler.DoCreate
// (its CanCreate enforces read-source + write-project); the only custom part is
// taking TaskID from the path rather than a request body.
func RegisterTaskDuplicateRoutes(api huma.API) {
	tags := []string{"tasks"}

	Register(api, huma.Operation{
		OperationID: "tasks-duplicate",
		Summary:     "Duplicate a task",
		Description: "Copies a task — including its labels, assignees, attachments, reminders and custom-field values — into the same project by default, or into another project when a project_id is given. Cross-project duplication requires a compatible custom-field destination and write access on the destination project, and records a \"copied from\" relation back to the original. The authenticated user needs read access to the source task and write access to the destination project. Returns the newly created duplicate.",
		Method:      http.MethodPost,
		Path:        "/tasks/{projecttask}/duplicate",
		Tags:        tags,
	}, tasksDuplicate)
}

func init() { AddRouteRegistrar(RegisterTaskDuplicateRoutes) }

func tasksDuplicate(ctx context.Context, in *struct {
	TaskID int64 `path:"projecttask" doc:"The numeric id of the task to duplicate."`
	// Pointer so the body is optional: duplicating into the same project needs
	// no request body at all.
	Body *struct {
		ProjectID int64 `json:"project_id,omitempty" doc:"The project to duplicate the task into. Defaults to the original task's project."`
	}
}) (*singleBody[models.TaskDuplicate], error) {
	a, err := authFromCtx(ctx)
	if err != nil {
		return nil, err
	}
	td := &models.TaskDuplicate{TaskID: in.TaskID}
	if in.Body != nil {
		td.ProjectID = in.Body.ProjectID
	}
	if err := handler.DoCreate(ctx, td, a); err != nil {
		return nil, translateDomainError(err)
	}
	return &singleBody[models.TaskDuplicate]{Body: td}, nil
}
