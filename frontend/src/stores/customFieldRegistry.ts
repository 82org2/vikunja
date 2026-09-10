import {computed, ref} from 'vue'
import {acceptHMRUpdate, defineStore} from 'pinia'

import type {ICustomFieldDefinition} from '@/modelTypes/ICustomFieldDefinition'
import type {ICustomFieldOption} from '@/modelTypes/ICustomFieldOption'
import type {ICustomFieldValue, ITaskCustomFieldValue} from '@/modelTypes/ICustomFieldValue'
import type {CustomFieldDefinition, CustomFieldOption, CustomFieldValue} from '@/client/generated'
import {
	listCustomFieldDefinitions,
	listCustomFieldOptions,
} from '@/services/customField'

function toValue(value: CustomFieldValue | null | undefined): ICustomFieldValue | null {
	if (!value) {
		return null
	}
	return {
		type: value.type ?? '',
		shortText: value.short_text,
		longText: value.long_text,
		number: value.number,
		boolean: value.boolean,
		date: value.date,
		datetime: value.datetime,
		url: value.url,
		userId: value.user_id,
		singleOptionId: value.single_option_id,
		optionIds: value.option_ids ?? undefined,
	}
}

function toDefinition(def: CustomFieldDefinition): ICustomFieldDefinition {
	return {
		id: def.id ?? 0,
		projectId: def.project_id ?? 0,
		machineKey: def.machine_key ?? '',
		title: def.title ?? '',
		description: def.description ?? '',
		fieldType: def.field_type ?? '',
		isArchived: def.is_archived ?? false,
		position: def.position ?? 0,
		showOnCard: def.show_on_card ?? false,
		showInTable: def.show_in_table ?? false,
		configuration: def.configuration ?? null,
		defaultValue: toValue(def.default_value),
		created: def.created ? new Date(def.created) : new Date(),
		updated: def.updated ? new Date(def.updated) : new Date(),
		maxPermission: null,
	}
}

function toOption(opt: CustomFieldOption): ICustomFieldOption {
	return {
		id: opt.id ?? 0,
		definitionId: opt.definition_id ?? 0,
		machineKey: opt.machine_key ?? '',
		label: opt.label ?? '',
		hexColor: opt.hex_color ?? '',
		isArchived: opt.is_archived ?? false,
		position: opt.position ?? 0,
		created: opt.created ? new Date(opt.created) : new Date(),
		updated: opt.updated ? new Date(opt.updated) : new Date(),
		maxPermission: null,
	}
}

// Project-scoped metadata registry for custom field definitions and their
// select options. Task responses only carry {definition_id, machine_key, value}
// wrappers, so editors, table headers, and cards resolve titles, display flags,
// configuration, and option labels/colors from here. Definitions and options are
// loaded per project (including archived ones, which must keep describing
// retained values) and cached; no per-task definition/option requests.
export const useCustomFieldRegistryStore = defineStore('customFieldRegistry', () => {
	const definitionsByProject = ref<{ [projectId: number]: ICustomFieldDefinition[] }>({})
	const optionsByDefinition = ref<{ [definitionId: number]: ICustomFieldOption[] }>({})
	const loadingProjects = ref<{ [projectId: number]: boolean }>({})

	const definitions = computed(() => Object.values(definitionsByProject.value).flat())
	const options = computed(() => Object.values(optionsByDefinition.value).flat())

	function getDefinitionsForProject(projectId: number): ICustomFieldDefinition[] {
		return definitionsByProject.value[projectId] ?? []
	}

	function getDefinitionById(definitionId: number): ICustomFieldDefinition | undefined {
		return definitions.value.find(d => d.id === definitionId)
	}

	function getOptionsForDefinition(definitionId: number): ICustomFieldOption[] {
		return optionsByDefinition.value[definitionId] ?? []
	}

	function getOptionById(optionId: number): ICustomFieldOption | undefined {
		return options.value.find(o => o.id === optionId)
	}

	function setDefinitionsForProject(projectId: number, defs: ICustomFieldDefinition[]) {
		definitionsByProject.value[projectId] = defs
	}

	function setOptionsForDefinition(definitionId: number, opts: ICustomFieldOption[]) {
		optionsByDefinition.value[definitionId] = opts
	}

	async function loadDefinitionsForProject(projectId: number): Promise<ICustomFieldDefinition[]> {
		if (loadingProjects.value[projectId]) {
			return getDefinitionsForProject(projectId)
		}
		loadingProjects.value[projectId] = true
		try {
			const defs = await listCustomFieldDefinitions(projectId, true)
			const converted = defs.map(toDefinition)
			setDefinitionsForProject(projectId, converted)
			return converted
		} finally {
			loadingProjects.value[projectId] = false
		}
	}

	async function loadOptionsForDefinition(projectId: number, definitionId: number): Promise<ICustomFieldOption[]> {
		const opts = await listCustomFieldOptions(projectId, definitionId, true)
		const converted = opts.map(toOption)
		setOptionsForDefinition(definitionId, converted)
		return converted
	}

	// Loads definitions and, for select definitions, their options. Used by
	// editors and view display so a single call populates everything needed.
	async function ensureProjectMetadata(projectId: number): Promise<void> {
		const defs = await loadDefinitionsForProject(projectId)
		await Promise.all(
			defs
				.filter(d => d.fieldType === 'single_select' || d.fieldType === 'multi_select')
				.map(d => loadOptionsForDefinition(projectId, d.id)),
		)
	}

	// Merges a task's populated value wrappers with the registry definitions so
	// callers get the definition context (title, type, config, options) alongside
	// the value. Definitions without a value row are included so unset editors
	// still render.
	function mergeTaskValues(
		projectId: number,
		taskValues: ITaskCustomFieldValue[],
	): {definition: ICustomFieldDefinition, value: ITaskCustomFieldValue | undefined}[] {
		const defs = getDefinitionsForProject(projectId)
		const byId = new Map(taskValues.map(v => [v.definitionId, v]))
		return defs.map(definition => ({
			definition,
			value: byId.get(definition.id),
		}))
	}

	function invalidateProject(projectId: number) {
		const defs = definitionsByProject.value[projectId] ?? []
		for (const d of defs) {
			delete optionsByDefinition.value[d.id]
		}
		delete definitionsByProject.value[projectId]
	}

	return {
		definitions,
		options,
		getDefinitionsForProject,
		getDefinitionById,
		getOptionsForDefinition,
		getOptionById,
		setDefinitionsForProject,
		setOptionsForDefinition,
		loadDefinitionsForProject,
		loadOptionsForDefinition,
		ensureProjectMetadata,
		mergeTaskValues,
		invalidateProject,
	}
})

// support hot reloading
if (import.meta.hot) {
	import.meta.hot.accept(acceptHMRUpdate(useCustomFieldRegistryStore, import.meta.hot))
}
