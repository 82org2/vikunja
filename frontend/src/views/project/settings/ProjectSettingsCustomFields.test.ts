import {describe, it, expect, beforeEach, vi} from 'vitest'
import {defineComponent, h} from 'vue'
import {mount, flushPromises} from '@vue/test-utils'
import {setActivePinia, createPinia} from 'pinia'
import {createI18n} from 'vue-i18n'
import {createRouter, createMemoryHistory} from 'vue-router'

import ProjectSettingsCustomFields from '@/views/project/settings/ProjectSettingsCustomFields.vue'
import {useBaseStore} from '@/stores/base'
import en from '@/i18n/lang/en.json'

const get = vi.fn(async () => ({id: 1, maxPermission: 2}))
vi.mock('@/services/project', () => ({
	default: class {
		loading = false
		get = get
	},
}))

const listDefinitions = vi.fn<(...args: unknown[]) => Promise<unknown[]>>(async () => [])
const listOptions = vi.fn<(...args: unknown[]) => Promise<unknown[]>>(async () => [])
vi.mock('@/services/customField', () => ({
	listCustomFieldDefinitions: (...args: unknown[]) => listDefinitions(...args),
	listCustomFieldOptions: (...args: unknown[]) => listOptions(...args),
	createCustomFieldDefinition: vi.fn(),
	updateCustomFieldDefinition: vi.fn(),
	archiveCustomFieldDefinition: vi.fn(),
	permanentlyDeleteCustomFieldDefinition: vi.fn(),
	createCustomFieldOption: vi.fn(),
	updateCustomFieldOption: vi.fn(),
	archiveCustomFieldOption: vi.fn(),
	setCustomFieldValue: vi.fn(),
	unsetCustomFieldValue: vi.fn(),
}))

const i18n = createI18n({legacy: false, locale: 'en', messages: {en}})

async function mountPage() {
	const errors: unknown[] = []
	const router = createRouter({
		history: createMemoryHistory(),
		routes: [{
			path: '/projects/:projectId/settings/custom-fields',
			name: 'project.settings.customFields',
			component: ProjectSettingsCustomFields,
		}],
	})
	await router.push('/projects/1/settings/custom-fields')
	await router.isReady()

	const Host = defineComponent({
		setup() {
			useBaseStore().setCurrentProject({id: 1, maxPermission: 2} as never)
			return () => h(ProjectSettingsCustomFields)
		},
	})

	const wrapper = mount(Host, {
		global: {
			plugins: [i18n, router],
			stubs: {
				CreateEdit: {template: '<div><slot /><slot name="footer" /></div>'},
				XButton: {template: '<button type="button"><slot /></button>'},
				BaseButton: {template: '<a><slot /></a>'},
				CustomTransition: {template: '<div><slot /></div>'},
				Modal: {template: '<div v-if="enabled"><slot name="header" /><slot name="text" /></div>', props: ['enabled']},
				Message: {template: '<div><slot /></div>'},
				FormField: {template: '<div><slot /></div>'},
				FormSelect: {template: '<div><slot /></div>'},
				FancyCheckbox: {template: '<label><slot /></label>'},
			},
			config: {
				errorHandler(err: unknown) {
					errors.push(err)
				},
			},
		},
	})
	await flushPromises()
	return {wrapper, errors}
}

describe('ProjectSettingsCustomFields', () => {
	beforeEach(() => {
		setActivePinia(createPinia())
		vi.clearAllMocks()
		listDefinitions.mockResolvedValue([])
		listOptions.mockResolvedValue([])
	})

	it('renders the create button for admins', async () => {
		const {wrapper, errors} = await mountPage()

		expect(errors).toEqual([])
		expect(wrapper.text()).toContain('Create custom field')
	})

	it('lists definitions loaded from the registry', async () => {
		listDefinitions.mockResolvedValue([{
			id: 1,
			project_id: 1,
			machine_key: 'priority',
			title: 'Priority',
			description: '',
			field_type: 'single_select',
			is_archived: false,
			position: 0,
			show_on_card: true,
			show_in_table: true,
			configuration: null,
			default_value: null,
			created: new Date().toISOString(),
			updated: new Date().toISOString(),
		}])

		const {wrapper} = await mountPage()

		expect(wrapper.text()).toContain('Priority')
		expect(wrapper.text()).toContain('priority')
	})

	it('shows archived definitions with the archived badge', async () => {
		listDefinitions.mockResolvedValue([{
			id: 2,
			project_id: 1,
			machine_key: 'old',
			title: 'Old field',
			description: '',
			field_type: 'short_text',
			is_archived: true,
			position: 0,
			show_on_card: false,
			show_in_table: false,
			configuration: null,
			default_value: null,
			created: new Date().toISOString(),
			updated: new Date().toISOString(),
		}])

		const {wrapper} = await mountPage()

		expect(wrapper.text()).toContain('Old field')
		expect(wrapper.text()).toContain('Archived')
	})
})
