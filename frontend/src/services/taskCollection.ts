import AbstractService from '@/services/abstractService'
import TaskModel from '@/models/task'
import BucketModel from '@/models/bucket'

import type {ITask} from '@/modelTypes/ITask'
import type {IBucket} from '@/modelTypes/IBucket'
import {apiV2Url} from '@/helpers/fetcher'

export type ExpandTaskFilterParam = 'subtasks' | 'buckets' | 'reactions' | 'comment_count' | 'is_unread' | 'custom_fields' | null

export interface TaskFilterParams {
	sort_by: string[],
	order_by: ('asc' | 'desc')[],
	filter: string,
	filter_include_nulls: boolean,
	filter_timezone?: string,
	s: string,
	per_page?: number,
	expand?: ExpandTaskFilterParam | ExpandTaskFilterParam[],
}

export function getDefaultTaskFilterParams(): TaskFilterParams {
	return {
		sort_by: ['position', 'id'],
		order_by: ['asc', 'desc'],
		filter: '',
		filter_include_nulls: false,
		filter_timezone: '',
		s: '',
		expand: 'subtasks',
	}
}

// v2 list endpoints take repeated query parameters (expand, sort_by, order_by)
// without the [] suffix axios adds by default, and search arrives as `q` rather
// than v1's `s`. This serializer emits repeated keys for arrays.
export function v2ParamsSerializer(params: Record<string, unknown>): string {
	const parts: string[] = []
	for (const [key, value] of Object.entries(params)) {
		if (value === undefined || value === null || value === '') {
			continue
		}
		if (Array.isArray(value)) {
			for (const item of value) {
				if (item === undefined || item === null || item === '') {
					continue
				}
				parts.push(`${encodeURIComponent(key)}=${encodeURIComponent(String(item))}`)
			}
			continue
		}
		if (typeof value === 'object') {
			parts.push(`${encodeURIComponent(key)}=${encodeURIComponent(JSON.stringify(value))}`)
			continue
		}
		parts.push(`${encodeURIComponent(key)}=${encodeURIComponent(String(value))}`)
	}
	return parts.join('&')
}

function v2TaskListUrl(model: {projectId?: number, viewId?: number}): string {
	if (model.projectId && model.viewId) {
		return apiV2Url(`projects/${model.projectId}/views/${model.viewId}/tasks`)
	}
	if (model.projectId) {
		return apiV2Url(`projects/${model.projectId}/tasks`)
	}
	return apiV2Url('tasks')
}

function v2TaskListQuery(params: TaskFilterParams, page: number): Record<string, unknown> {
	return {
		page,
		per_page: params.per_page,
		q: params.s,
		filter: params.filter,
		filter_include_nulls: params.filter_include_nulls,
		filter_timezone: params.filter_timezone,
		sort_by: params.sort_by,
		order_by: params.order_by,
		expand: params.expand,
	}
}

export default class TaskCollectionService extends AbstractService<ITask> {
	constructor() {
		super({
			getAll: '/projects/{projectId}/views/{viewId}/tasks',
		})
	}

	modelFactory(data: Record<string, unknown>) {
		return new TaskModel(data)
	}

	// v2 task-list reads: parses the {items, total, page, per_page, total_pages}
	// envelope and normalizes each item through TaskModel (which parses
	// custom_fields). The v1 x-pagination-* headers no longer apply.
	async getAll(model: {projectId?: number, viewId?: number} = {}, params: TaskFilterParams = getDefaultTaskFilterParams(), page = 1): Promise<ITask[]> {
		const cancel = this.setLoading()

		try {
			const response = await this.http.get(v2TaskListUrl(model), {
				params: v2TaskListQuery(params, page),
				paramsSerializer: v2ParamsSerializer,
			})
			const data = response.data
			this.totalPages = data.total_pages ?? 1
			this.resultCount = data.items?.length ?? 0
			return (data.items ?? []).map((entry: Record<string, unknown>) => this.modelGetAllFactory(entry))
		} finally {
			cancel()
		}
	}

	// v2 buckets-with-tasks: the response is {items: [{...bucket, tasks}], total}
	// where total is the bucket count, not a task count. Not paginated.
	async getBucketsWithTasks(projectId: number, viewId: number, params: TaskFilterParams = getDefaultTaskFilterParams()): Promise<IBucket[]> {
		const cancel = this.setLoading()

		try {
			const response = await this.http.get(apiV2Url(`projects/${projectId}/views/${viewId}/buckets/tasks`), {
				params: v2TaskListQuery(params, 1),
				paramsSerializer: v2ParamsSerializer,
			})
			const data = response.data
			return (data.items ?? []).map((b: Record<string, unknown>) => new BucketModel(b))
		} finally {
			cancel()
		}
	}
}
