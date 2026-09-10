import {describe, it, expect, beforeEach, vi} from 'vitest'
import {mount} from '@vue/test-utils'
import {setActivePinia, createPinia} from 'pinia'

import CustomFieldTableCell from '@/components/tasks/partials/CustomFieldTableCell.vue'
import {useCustomFieldRegistryStore} from '@/stores/customFieldRegistry'

function mountCell(value: unknown) {
	return mount(CustomFieldTableCell, {
		props: {value: value as never},
		global: {
			stubs: {
				Icon: {template: '<span class="icon-stub" />'},
			},
		},
	})
}

describe('CustomFieldTableCell', () => {
	beforeEach(() => {
		setActivePinia(createPinia())
	})

	it('renders short text', () => {
		const wrapper = mountCell({type: 'short_text', shortText: 'hello'})
		expect(wrapper.text()).toContain('hello')
	})

	it('renders a number', () => {
		const wrapper = mountCell({type: 'number', number: 42})
		expect(wrapper.text()).toContain('42')
	})

	it('renders a boolean', () => {
		const wrapper = mountCell({type: 'boolean', boolean: true})
		expect(wrapper.find('svg').exists()).toBe(true)
	})

	it('renders a date', () => {
		const wrapper = mountCell({type: 'date', date: '2026-09-10'})
		expect(wrapper.text()).toContain('2026-09-10')
	})

	it('renders a url as a link', () => {
		const wrapper = mountCell({type: 'url', url: 'https://example.com'})
		const link = wrapper.find('a')
		expect(link.attributes('href')).toBe('https://example.com')
		expect(link.attributes('target')).toBe('_blank')
	})

	it('renders a single select option label from the registry', () => {
		const registry = useCustomFieldRegistryStore()
		registry.setOptionsForDefinition(1, [{
			id: 5,
			definitionId: 1,
			machineKey: 'opt',
			label: 'High',
			hexColor: 'ff0000',
			isArchived: false,
			position: 0,
			maxPermission: null,
			created: new Date(),
			updated: new Date(),
		}])

		const wrapper = mountCell({type: 'single_select', singleOptionId: 5})
		expect(wrapper.text()).toContain('High')
	})

	it('renders a dash for an empty value', () => {
		const wrapper = mountCell(null)
		expect(wrapper.text()).toContain('-')
	})
})
