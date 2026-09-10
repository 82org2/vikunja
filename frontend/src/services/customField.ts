import {
	customFieldDefinitionsCreate,
	customFieldDefinitionsDelete,
	customFieldDefinitionsList,
	customFieldDefinitionsPermanentDelete,
	customFieldDefinitionsUpdate,
	customFieldOptionsCreate,
	customFieldOptionsDelete,
	customFieldOptionsList,
	customFieldOptionsUpdate,
	customFieldValuesSet,
	customFieldValuesUnset,
} from '@/client/generated'
import type {
	CustomFieldDefinition,
	CustomFieldDefinitionReadBodyWritable,
	CustomFieldDefinitionWritable,
	CustomFieldOption,
	CustomFieldOptionReadBodyWritable,
	CustomFieldOptionWritable,
	CustomFieldValue,
	CustomFieldValueWritable,
} from '@/client/generated'

const DEFINITIONS_PER_PAGE = 100
const OPTIONS_PER_PAGE = 200

export async function listCustomFieldDefinitions(
	projectId: number,
	includeArchived = true,
): Promise<CustomFieldDefinition[]> {
	const result: CustomFieldDefinition[] = []
	let page = 1
	while (true) {
		const {data} = await customFieldDefinitionsList({
			path: {project: projectId},
			query: {include_archived: includeArchived, page, per_page: DEFINITIONS_PER_PAGE},
		})
		result.push(...(data.items ?? []))
		if (page >= (data.total_pages ?? 1)) {
			break
		}
		page++
	}
	return result
}

export async function createCustomFieldDefinition(
	projectId: number,
	body: CustomFieldDefinitionWritable,
): Promise<CustomFieldDefinition> {
	const {data} = await customFieldDefinitionsCreate({path: {project: projectId}, body})
	return data
}

export async function updateCustomFieldDefinition(
	projectId: number,
	definitionId: number,
	body: CustomFieldDefinitionReadBodyWritable,
): Promise<CustomFieldDefinition> {
	const {data} = await customFieldDefinitionsUpdate({path: {project: projectId, definition: definitionId}, body})
	return data
}

export async function archiveCustomFieldDefinition(projectId: number, definitionId: number): Promise<void> {
	await customFieldDefinitionsDelete({path: {project: projectId, definition: definitionId}})
}

export async function permanentlyDeleteCustomFieldDefinition(
	projectId: number,
	definitionId: number,
	deleteValues: boolean,
): Promise<void> {
	await customFieldDefinitionsPermanentDelete({
		path: {project: projectId, definition: definitionId},
		body: {delete_values: deleteValues},
	})
}

export async function listCustomFieldOptions(
	projectId: number,
	definitionId: number,
	includeArchived = true,
): Promise<CustomFieldOption[]> {
	const result: CustomFieldOption[] = []
	let page = 1
	while (true) {
		const {data} = await customFieldOptionsList({
			path: {project: projectId, definition: definitionId},
			query: {include_archived: includeArchived, page, per_page: OPTIONS_PER_PAGE},
		})
		result.push(...(data.items ?? []))
		if (page >= (data.total_pages ?? 1)) {
			break
		}
		page++
	}
	return result
}

export async function createCustomFieldOption(
	projectId: number,
	definitionId: number,
	body: CustomFieldOptionWritable,
): Promise<CustomFieldOption> {
	const {data} = await customFieldOptionsCreate({path: {project: projectId, definition: definitionId}, body})
	return data
}

export async function updateCustomFieldOption(
	projectId: number,
	definitionId: number,
	optionId: number,
	body: CustomFieldOptionReadBodyWritable,
): Promise<CustomFieldOption> {
	const {data} = await customFieldOptionsUpdate({path: {project: projectId, definition: definitionId, option: optionId}, body})
	return data
}

export async function archiveCustomFieldOption(projectId: number, definitionId: number, optionId: number): Promise<void> {
	await customFieldOptionsDelete({path: {project: projectId, definition: definitionId, option: optionId}})
}

export async function setCustomFieldValue(
	taskId: number,
	definitionId: number,
	value: CustomFieldValueWritable,
): Promise<CustomFieldValue> {
	const {data} = await customFieldValuesSet({path: {task: taskId, definition: definitionId}, body: value})
	return data
}

export async function unsetCustomFieldValue(taskId: number, definitionId: number): Promise<void> {
	await customFieldValuesUnset({path: {task: taskId, definition: definitionId}})
}
