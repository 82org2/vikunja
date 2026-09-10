import {describe, it, expect, beforeEach, vi} from 'vitest'

import TaskCollectionService, {v2ParamsSerializer} from '@/services/taskCollection'

const {get} = vi.hoisted(() => ({get: vi.fn()}))

vi.mock('@/helpers/fetcher', () => ({
	apiV2Url: (path: string) => `https://api.test/api/v2/${path}`,
	AuthenticatedHTTPFactory: () => ({
		get,
		interceptors: {
			request: {use: vi.fn()},
			response: {use: vi.fn()},
		},
	}),
}))

describe('v2ParamsSerializer', () => {
	it('emits repeated keys for arrays without the [] suffix', () => {
		const qs = v2ParamsSerializer({
			expand: ['subtasks', 'custom_fields'],
			sort_by: ['position', 'id'],
			page: 1,
		})
		expect(qs).toBe('expand=subtasks&expand=custom_fields&sort_by=position&sort_by=id&page=1')
	})

	it('skips empty and undefined values', () => {
		const qs = v2ParamsSerializer({s: '', filter: undefined, page: 1})
		expect(qs).toBe('page=1')
	})
})

describe('TaskCollectionService v2 getAll', () => {
	beforeEach(() => {
		get.mockReset()
	})

	it('parses the v2 paginated envelope and sets totalPages', async () => {
		get.mockResolvedValue({
			data: {
				items: [{id: 1, title: 'Task 1', project_id: 1}],
				total: 1,
				page: 1,
				per_page: 50,
				total_pages: 1,
			},
		})

		const service = new TaskCollectionService()
		const tasks = await service.getAll({projectId: 1, viewId: 2}, {
			sort_by: ['position'],
			order_by: ['asc'],
			filter: '',
			filter_include_nulls: false,
			s: '',
			expand: ['custom_fields'],
		})

		expect(tasks).toHaveLength(1)
		expect(tasks[0].id).toBe(1)
		expect(service.totalPages).toBe(1)
		expect(service.resultCount).toBe(1)
	})

	it('uses the view-scoped v2 url', async () => {
		get.mockResolvedValue({data: {items: [], total: 0, page: 1, per_page: 50, total_pages: 1}})

		const service = new TaskCollectionService()
		await service.getAll({projectId: 1, viewId: 2}, {
			sort_by: ['position'],
			order_by: ['asc'],
			filter: '',
			filter_include_nulls: false,
			s: '',
		})

		expect(get.mock.calls[0][0]).toBe('https://api.test/api/v2/projects/1/views/2/tasks')
	})

	it('uses the project-scoped v2 url when no view is given', async () => {
		get.mockResolvedValue({data: {items: [], total: 0, page: 1, per_page: 50, total_pages: 1}})

		const service = new TaskCollectionService()
		await service.getAll({projectId: 1}, {
			sort_by: ['position'],
			order_by: ['asc'],
			filter: '',
			filter_include_nulls: false,
			s: '',
		})

		expect(get.mock.calls[0][0]).toBe('https://api.test/api/v2/projects/1/tasks')
	})

	it('parses the buckets-with-tasks envelope', async () => {
		get.mockResolvedValue({
			data: {
				items: [{id: 1, title: 'Bucket', tasks: [{id: 1, title: 'Task', project_id: 1}]}],
				total: 1,
			},
		})

		const service = new TaskCollectionService()
		const buckets = await service.getBucketsWithTasks(1, 2, {
			sort_by: ['position'],
			order_by: ['asc'],
			filter: '',
			filter_include_nulls: false,
			s: '',
		})

		expect(buckets).toHaveLength(1)
		expect(buckets[0].id).toBe(1)
		expect(buckets[0].tasks).toHaveLength(1)
		expect(get.mock.calls[0][0]).toBe('https://api.test/api/v2/projects/1/views/2/buckets/tasks')
	})
})
