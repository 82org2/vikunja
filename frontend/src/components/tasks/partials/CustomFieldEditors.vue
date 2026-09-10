<template>
	<div class="custom-field-editors">
		<div
			v-for="entry in merged"
			:key="entry.definition.id"
			class="custom-field-editor"
		>
			<div class="detail-title">
				{{ entry.definition.title }}
				<span
					v-if="entry.definition.isArchived"
					class="tag is-warning is-small"
				>{{ $t('project.customFields.archived') }}</span>
			</div>
			<div class="custom-field-editor__control">
				<FormField
					v-if="entry.definition.fieldType === 'short_text'"
					:model-value="textValue(entry)"
					:disabled="!canWrite || entry.definition.isArchived"
					:label="entry.definition.title"
					@update:modelValue="v => setTextValue(entry, v)"
				/>
				<FormField
					v-else-if="entry.definition.fieldType === 'long_text'"
					:model-value="textValue(entry)"
					:disabled="!canWrite || entry.definition.isArchived"
					:label="entry.definition.title"
					@update:modelValue="v => setTextValue(entry, v)"
				/>
				<FormField
					v-else-if="entry.definition.fieldType === 'number'"
					:model-value="numberText(entry)"
					:disabled="!canWrite || entry.definition.isArchived"
					:label="entry.definition.title"
					@update:modelValue="v => setNumberValue(entry, v)"
				/>
				<FancyCheckbox
					v-else-if="entry.definition.fieldType === 'boolean'"
					:model-value="booleanValue(entry)"
					:disabled="!canWrite || entry.definition.isArchived"
					@update:modelValue="v => setBooleanValue(entry, v)"
				>
					{{ entry.definition.title }}
				</FancyCheckbox>
				<FormField
					v-else-if="entry.definition.fieldType === 'date'"
					:model-value="dateValue(entry)"
					:disabled="!canWrite || entry.definition.isArchived"
					:label="entry.definition.title"
					@update:modelValue="v => setDateValue(entry, v)"
				/>
				<FormField
					v-else-if="entry.definition.fieldType === 'datetime'"
					:model-value="datetimeValue(entry)"
					:disabled="!canWrite || entry.definition.isArchived"
					:label="entry.definition.title"
					@update:modelValue="v => setDatetimeValue(entry, v)"
				/>
				<FormField
					v-else-if="entry.definition.fieldType === 'url'"
					:model-value="textValue(entry)"
					:disabled="!canWrite || entry.definition.isArchived"
					:label="entry.definition.title"
					@update:modelValue="v => setTextValue(entry, v)"
				/>
				<FormSelect
					v-else-if="entry.definition.fieldType === 'single_select'"
					:model-value="singleSelectValue(entry)"
					:disabled="!canWrite || entry.definition.isArchived"
					:label="entry.definition.title"
					:options="selectOptions(entry)"
					@update:modelValue="v => setSingleSelectValue(entry, v)"
				/>
				<FormSelect
					v-else-if="entry.definition.fieldType === 'multi_select'"
					:model-value="multiSelectValue(entry)"
					:disabled="!canWrite || entry.definition.isArchived"
					:label="entry.definition.title"
					:options="selectOptions(entry)"
					@update:modelValue="v => setMultiSelectValue(entry, v)"
				/>
				<FormField
					v-else-if="entry.definition.fieldType === 'user'"
					:model-value="userValue(entry)"
					:disabled="!canWrite || entry.definition.isArchived"
					:label="entry.definition.title"
					@update:modelValue="v => setUserValue(entry, v)"
				/>
				<XButton
					v-if="canWrite && entry.value && !entry.definition.isArchived"
					variant="tertiary"
					class="is-small"
					:aria-label="$t('task.customFields.unset')"
					@click="unsetValue(entry)"
				>
					{{ $t('task.customFields.unset') }}
				</XButton>
			</div>
		</div>
	</div>
</template>

<script setup lang="ts">
import {computed, watch} from 'vue'
import FormField from '@/components/input/FormField.vue'
import FormSelect from '@/components/input/FormSelect.vue'
import FancyCheckbox from '@/components/input/FancyCheckbox.vue'
import XButton from '@/components/input/Button.vue'
import type {ITask} from '@/modelTypes/ITask'
import type {ICustomFieldDefinition} from '@/modelTypes/ICustomFieldDefinition'
import type {ICustomFieldOption} from '@/modelTypes/ICustomFieldOption'
import type {ICustomFieldValue, ITaskCustomFieldValue} from '@/modelTypes/ICustomFieldValue'
import type {CustomFieldValue, CustomFieldValueWritable} from '@/client/generated'
import {useCustomFieldRegistryStore} from '@/stores/customFieldRegistry'
import {setCustomFieldValue, unsetCustomFieldValue} from '@/services/customField'
import {error} from '@/message'

const props = defineProps<{
	task: ITask,
	canWrite: boolean,
}>()

const emit = defineEmits<{
	'valueChanged': [payload: {definitionId: number, value: ITaskCustomFieldValue | null}]
}>()

const registry = useCustomFieldRegistryStore()

const merged = computed(() => {
	if (!props.task.projectId) {
		return []
	}
	return registry.mergeTaskValues(props.task.projectId, props.task.customFields ?? [])
})

watch(
	() => props.task.projectId,
	async (projectId) => {
		if (projectId) {
			await registry.ensureProjectMetadata(projectId)
		}
	},
	{immediate: true},
)

function textValue(entry: {definition: ICustomFieldDefinition, value: ITaskCustomFieldValue | undefined}): string {
	const v = entry.value?.value
	if (v?.type === 'short_text') return v.shortText ?? ''
	if (v?.type === 'long_text') return v.longText ?? ''
	if (v?.type === 'url') return v.url ?? ''
	return ''
}

function numberText(entry: {definition: ICustomFieldDefinition, value: ITaskCustomFieldValue | undefined}): string {
	const v = entry.value?.value
	if (v?.type === 'number' && typeof v.number !== 'undefined') {
		return String(v.number)
	}
	return ''
}

function booleanValue(entry: {definition: ICustomFieldDefinition, value: ITaskCustomFieldValue | undefined}): boolean {
	const v = entry.value?.value
	return v?.type === 'boolean' ? Boolean(v.boolean) : false
}

function dateValue(entry: {definition: ICustomFieldDefinition, value: ITaskCustomFieldValue | undefined}): string {
	const v = entry.value?.value
	return v?.type === 'date' ? (v.date ?? '') : ''
}

function datetimeValue(entry: {definition: ICustomFieldDefinition, value: ITaskCustomFieldValue | undefined}): string {
	const v = entry.value?.value
	return v?.type === 'datetime' ? (v.datetime ?? '') : ''
}

function singleSelectValue(entry: {definition: ICustomFieldDefinition, value: ITaskCustomFieldValue | undefined}): number | '' {
	const v = entry.value?.value
	return v?.type === 'single_select' && typeof v.singleOptionId !== 'undefined' ? v.singleOptionId : ''
}

function multiSelectValue(entry: {definition: ICustomFieldDefinition, value: ITaskCustomFieldValue | undefined}): number | '' {
	const v = entry.value?.value
	return v?.type === 'multi_select' && v.optionIds?.length ? v.optionIds[0] : ''
}

function userValue(entry: {definition: ICustomFieldDefinition, value: ITaskCustomFieldValue | undefined}): string {
	const v = entry.value?.value
	return v?.type === 'user' && typeof v.userId !== 'undefined' ? String(v.userId) : ''
}

function selectOptions(entry: {definition: ICustomFieldDefinition, value: ITaskCustomFieldValue | undefined}): {value: string | number, label: string, disabled?: boolean}[] {
	return registry.getOptionsForDefinition(entry.definition.id)
		.map((opt: ICustomFieldOption) => ({
			value: opt.id,
			label: opt.label,
			disabled: opt.isArchived,
		}))
}

// The service layer speaks the snake_case wire shape; the task model carries
// camelCase. Convert between them at the boundary.
function toWireValue(value: ICustomFieldValue): CustomFieldValueWritable {
	const wire: CustomFieldValueWritable = {type: value.type}
	if (value.type === 'short_text') wire.short_text = value.shortText
	if (value.type === 'long_text') wire.long_text = value.longText
	if (value.type === 'number') wire.number = value.number
	if (value.type === 'boolean') wire.boolean = value.boolean
	if (value.type === 'date') wire.date = value.date
	if (value.type === 'datetime') wire.datetime = value.datetime
	if (value.type === 'url') wire.url = value.url
	if (value.type === 'user') wire.user_id = value.userId
	if (value.type === 'single_select') wire.single_option_id = value.singleOptionId
	if (value.type === 'multi_select') wire.option_ids = value.optionIds
	return wire
}

function fromWireValue(value: CustomFieldValue): ICustomFieldValue {
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

async function writeValue(entry: {definition: ICustomFieldDefinition, value: ITaskCustomFieldValue | undefined}, value: ICustomFieldValue) {
	try {
		const saved = await setCustomFieldValue(props.task.id, entry.definition.id, toWireValue(value))
		emit('valueChanged', {
			definitionId: entry.definition.id,
			value: {
				definitionId: entry.definition.id,
				machineKey: entry.definition.machineKey,
				value: fromWireValue(saved),
			},
		})
	} catch (e) {
		error(e)
	}
}

function setTextValue(entry: {definition: ICustomFieldDefinition, value: ITaskCustomFieldValue | undefined}, v: string | number) {
	const text = String(v)
	const type = entry.definition.fieldType
	if (type === 'short_text') {
		writeValue(entry, {type, shortText: text})
	} else if (type === 'long_text') {
		writeValue(entry, {type, longText: text})
	} else if (type === 'url') {
		writeValue(entry, {type, url: text})
	}
}

function setNumberValue(entry: {definition: ICustomFieldDefinition, value: ITaskCustomFieldValue | undefined}, v: string | number) {
	if (v === '') {
		unsetValue(entry)
		return
	}
	writeValue(entry, {type: 'number', number: Number(v)})
}

function setBooleanValue(entry: {definition: ICustomFieldDefinition, value: ITaskCustomFieldValue | undefined}, v: boolean) {
	writeValue(entry, {type: 'boolean', boolean: v})
}

function setDateValue(entry: {definition: ICustomFieldDefinition, value: ITaskCustomFieldValue | undefined}, v: string | number) {
	if (v === '') {
		unsetValue(entry)
		return
	}
	writeValue(entry, {type: 'date', date: String(v)})
}

function setDatetimeValue(entry: {definition: ICustomFieldDefinition, value: ITaskCustomFieldValue | undefined}, v: string | number) {
	if (v === '') {
		unsetValue(entry)
		return
	}
	writeValue(entry, {type: 'datetime', datetime: String(v)})
}

function setSingleSelectValue(entry: {definition: ICustomFieldDefinition, value: ITaskCustomFieldValue | undefined}, v: string | number) {
	if (v === '') {
		unsetValue(entry)
		return
	}
	writeValue(entry, {type: 'single_select', singleOptionId: Number(v)})
}

function setMultiSelectValue(entry: {definition: ICustomFieldDefinition, value: ITaskCustomFieldValue | undefined}, v: string | number) {
	if (v === '') {
		unsetValue(entry)
		return
	}
	writeValue(entry, {type: 'multi_select', optionIds: [Number(v)]})
}

function setUserValue(entry: {definition: ICustomFieldDefinition, value: ITaskCustomFieldValue | undefined}, v: string | number) {
	if (v === '') {
		unsetValue(entry)
		return
	}
	writeValue(entry, {type: 'user', userId: Number(v)})
}

async function unsetValue(entry: {definition: ICustomFieldDefinition, value: ITaskCustomFieldValue | undefined}) {
	try {
		await unsetCustomFieldValue(props.task.id, entry.definition.id)
		emit('valueChanged', {definitionId: entry.definition.id, value: null})
	} catch (e) {
		error(e)
	}
}
</script>

<style lang="scss" scoped>
.custom-field-editors {
	display: flex;
	flex-direction: column;
	gap: 1rem;
}

.custom-field-editor {
	display: flex;
	flex-direction: column;
	gap: .25rem;
}

.custom-field-editor__control {
	display: flex;
	align-items: center;
	gap: .5rem;
}
</style>
