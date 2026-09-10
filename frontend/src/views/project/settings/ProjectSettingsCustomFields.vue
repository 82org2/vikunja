<script lang="ts" setup>
import {computed, ref, watchEffect} from 'vue'
import {useRoute} from 'vue-router'
import {useI18n} from 'vue-i18n'
import {useTitle} from '@vueuse/core'

import ProjectService from '@/services/project'
import ProjectModel from '@/models/project'
import type {IProject} from '@/modelTypes/IProject'
import type {ICustomFieldDefinition} from '@/modelTypes/ICustomFieldDefinition'
import type {ICustomFieldOption} from '@/modelTypes/ICustomFieldOption'

import CreateEdit from '@/components/misc/CreateEdit.vue'
import XButton from '@/components/input/Button.vue'
import FormField from '@/components/input/FormField.vue'
import FormSelect from '@/components/input/FormSelect.vue'
import FancyCheckbox from '@/components/input/FancyCheckbox.vue'
import Modal from '@/components/misc/Modal.vue'
import Message from '@/components/misc/Message.vue'

import {useBaseStore} from '@/stores/base'
import {useCustomFieldRegistryStore} from '@/stores/customFieldRegistry'
import {success, error} from '@/message'
import {PERMISSIONS} from '@/constants/permissions'
import {CUSTOM_FIELD_TYPES} from '@/modelTypes/ICustomFieldValue'
import {
	createCustomFieldDefinition,
	updateCustomFieldDefinition,
	permanentlyDeleteCustomFieldDefinition,
	createCustomFieldOption,
	updateCustomFieldOption,
	archiveCustomFieldOption,
} from '@/services/customField'

defineOptions({name: 'ProjectSettingsCustomFields'})

const {t} = useI18n({useScope: 'global'})

const project = ref<IProject>()
useTitle(t('project.customFields.title'))

const route = useRoute()
const projectId = computed(() => route.params.projectId !== undefined
	? parseInt(route.params.projectId as string)
	: undefined,
)

const registry = useCustomFieldRegistryStore()
const isAdmin = ref(false)
const loading = ref(false)

async function loadProject(id: number) {
	const projectService = new ProjectService()
	const newProject = await projectService.get(new ProjectModel({id}))
	await useBaseStore().handleSetCurrentProject({project: newProject})
	project.value = newProject
	isAdmin.value = newProject.maxPermission === PERMISSIONS.ADMIN
	await loadDefinitions()
}

watchEffect(() => projectId.value !== undefined && loadProject(projectId.value))

const definitions = ref<ICustomFieldDefinition[]>([])

async function loadDefinitions() {
	if (projectId.value === undefined) {
		return
	}
	loading.value = true
	try {
		definitions.value = await registry.loadDefinitionsForProject(projectId.value)
	} finally {
		loading.value = false
	}
}

// --- definition create/edit ---

const showCreateForm = ref(false)
const editingDefinition = ref<ICustomFieldDefinition | null>(null)
const definitionForm = ref<{
	title: string,
	description: string,
	machineKey: string,
	fieldType: string,
	showOnCard: boolean,
	showInTable: boolean,
	precision: string,
	min: string,
	max: string,
	step: string,
	unit: string,
}>({
	title: '',
	description: '',
	machineKey: '',
	fieldType: CUSTOM_FIELD_TYPES.SHORT_TEXT,
	showOnCard: false,
	showInTable: false,
	precision: '0',
	min: '',
	max: '',
	step: '',
	unit: '',
})

function resetDefinitionForm() {
	definitionForm.value = {
		title: '',
		description: '',
		machineKey: '',
		fieldType: CUSTOM_FIELD_TYPES.SHORT_TEXT,
		showOnCard: false,
		showInTable: false,
		precision: '0',
		min: '',
		max: '',
		step: '',
		unit: '',
	}
}

function startCreate() {
	resetDefinitionForm()
	showCreateForm.value = true
	editingDefinition.value = null
}

function startEdit(def: ICustomFieldDefinition) {
	editingDefinition.value = def
	definitionForm.value = {
		title: def.title,
		description: def.description,
		machineKey: def.machineKey,
		fieldType: def.fieldType,
		showOnCard: def.showOnCard,
		showInTable: def.showInTable,
		precision: String(def.configuration?.precision ?? 0),
		min: def.configuration?.min ?? '',
		max: def.configuration?.max ?? '',
		step: def.configuration?.step ?? '',
		unit: def.configuration?.unit ?? '',
	}
	showCreateForm.value = true
}

function definitionBody() {
	const f = definitionForm.value
	const body: Record<string, unknown> = {
		title: f.title,
		description: f.description,
		show_on_card: f.showOnCard,
		show_in_table: f.showInTable,
	}
	// The backend validates machine_key and field_type against the stored row
	// on every update, so they must be echoed even though they are immutable.
	if (editingDefinition.value) {
		body.machine_key = editingDefinition.value.machineKey
		body.field_type = editingDefinition.value.fieldType
	}
	if (f.fieldType === CUSTOM_FIELD_TYPES.NUMBER) {
		const configuration: Record<string, unknown> = {precision: Number(f.precision)}
		if (f.min !== '') configuration.min = f.min
		if (f.max !== '') configuration.max = f.max
		if (f.step !== '') configuration.step = f.step
		if (f.unit !== '') configuration.unit = f.unit
		body.configuration = configuration
	}
	return body
}

async function saveDefinition() {
	if (projectId.value === undefined) {
		return
	}
	try {
		if (editingDefinition.value) {
			await updateCustomFieldDefinition(projectId.value, editingDefinition.value.id, definitionBody())
			success({message: t('project.customFields.updateSuccess')})
		} else {
			await createCustomFieldDefinition(projectId.value, {
				...definitionBody(),
				machine_key: definitionForm.value.machineKey,
				field_type: definitionForm.value.fieldType,
			})
			success({message: t('project.customFields.createSuccess')})
		}
		showCreateForm.value = false
		editingDefinition.value = null
		registry.invalidateProject(projectId.value)
		await loadDefinitions()
	} catch (e) {
		error(e)
	}
}

async function toggleArchive(def: ICustomFieldDefinition) {
	if (projectId.value === undefined) {
		return
	}
	try {
		// The backend validates machine_key and field_type against the stored
		// row on every update, so the full definition must be sent even when
		// only the archive flag changes.
		const body = {
			title: def.title,
			description: def.description,
			machine_key: def.machineKey,
			field_type: def.fieldType,
			is_archived: !def.isArchived,
			show_on_card: def.showOnCard,
			show_in_table: def.showInTable,
		}
		await updateCustomFieldDefinition(projectId.value, def.id, body)
		success({message: t(def.isArchived ? 'project.customFields.unarchiveSuccess' : 'project.customFields.archiveSuccess')})
		registry.invalidateProject(projectId.value)
		await loadDefinitions()
	} catch (e) {
		error(e)
	}
}

// --- permanent delete (two-stage) ---

const definitionToDelete = ref<ICustomFieldDefinition | null>(null)
const showDeleteModal = ref(false)
const deleteValues = ref(false)
const deleteConflict = ref(false)

function startDelete(def: ICustomFieldDefinition) {
	definitionToDelete.value = def
	deleteValues.value = false
	deleteConflict.value = false
	showDeleteModal.value = true
}

async function confirmDelete() {
	if (projectId.value === undefined || definitionToDelete.value === null) {
		return
	}
	try {
		await permanentlyDeleteCustomFieldDefinition(projectId.value, definitionToDelete.value.id, deleteValues.value)
		success({message: t('project.customFields.deleteSuccess')})
		showDeleteModal.value = false
		definitionToDelete.value = null
		registry.invalidateProject(projectId.value)
		await loadDefinitions()
	} catch (e) {
		// First attempt without delete_values returns a conflict when values
		// exist; surface the destructive confirmation instead of failing.
		const status = (e as {response?: {status?: number}, status?: number})?.response?.status
			?? (e as {status?: number})?.status
		if (status === 409) {
			deleteConflict.value = true
			return
		}
		error(e)
	}
}

// --- options management ---

const optionsForDefinition = ref<ICustomFieldDefinition | null>(null)
const options = ref<ICustomFieldOption[]>([])
const showOptionsModal = ref(false)
const optionForm = ref<{label: string, machineKey: string, hexColor: string}>({
	label: '',
	machineKey: '',
	hexColor: '',
})
const editingOption = ref<ICustomFieldOption | null>(null)

async function openOptions(def: ICustomFieldDefinition) {
	if (projectId.value === undefined) {
		return
	}
	optionsForDefinition.value = def
	options.value = await registry.loadOptionsForDefinition(projectId.value, def.id)
	showOptionsModal.value = true
}

function resetOptionForm() {
	optionForm.value = {label: '', machineKey: '', hexColor: ''}
	editingOption.value = null
}

function startEditOption(opt: ICustomFieldOption) {
	editingOption.value = opt
	optionForm.value = {
		label: opt.label,
		machineKey: opt.machineKey,
		hexColor: opt.hexColor,
	}
}

async function saveOption() {
	if (projectId.value === undefined || optionsForDefinition.value === null) {
		return
	}
	const defId = optionsForDefinition.value.id
	try {
		if (editingOption.value) {
			await updateCustomFieldOption(projectId.value, defId, editingOption.value.id, {
				label: optionForm.value.label,
				hex_color: optionForm.value.hexColor,
			})
			success({message: t('project.customFields.optionUpdateSuccess')})
		} else {
			await createCustomFieldOption(projectId.value, defId, {
				label: optionForm.value.label,
				machine_key: optionForm.value.machineKey,
				hex_color: optionForm.value.hexColor,
			})
			success({message: t('project.customFields.optionCreateSuccess')})
		}
		resetOptionForm()
		options.value = await registry.loadOptionsForDefinition(projectId.value, defId)
	} catch (e) {
		error(e)
	}
}

async function toggleOptionArchive(opt: ICustomFieldOption) {
	if (projectId.value === undefined || optionsForDefinition.value === null) {
		return
	}
	const defId = optionsForDefinition.value.id
	try {
		if (opt.isArchived) {
			await updateCustomFieldOption(projectId.value, defId, opt.id, {
				label: opt.label,
				hex_color: opt.hexColor,
				is_archived: false,
			})
		} else {
			await archiveCustomFieldOption(projectId.value, defId, opt.id)
		}
		options.value = await registry.loadOptionsForDefinition(projectId.value, defId)
	} catch (e) {
		error(e)
	}
}

const fieldTypeOptions = Object.values(CUSTOM_FIELD_TYPES).map(type => ({
	value: type,
	label: t(`project.customFields.types.${type}`),
}))

function isSelectFieldType(type: string): boolean {
	return type === CUSTOM_FIELD_TYPES.SINGLE_SELECT || type === CUSTOM_FIELD_TYPES.MULTI_SELECT
}
</script>

<template>
	<CreateEdit
		:title="$t('project.customFields.title')"
		:has-primary-action="false"
		:wide="true"
		:loading="loading"
	>
		<Message v-if="!isAdmin">
			{{ $t('project.customFields.onlyAdminsCanEdit') }}
		</Message>

		<div
			v-if="isAdmin"
			class="is-flex is-justify-content-end mbe-4"
		>
			<XButton @click="startCreate">
				{{ $t('project.customFields.create') }}
			</XButton>
		</div>

		<div
			v-if="showCreateForm && isAdmin"
			class="box mbe-4"
		>
			<h3 class="title is-5">
				{{ editingDefinition ? $t('project.customFields.edit') : $t('project.customFields.create') }}
			</h3>
			<FormField
				v-model="definitionForm.title"
				:label="$t('project.customFields.title')"
			/>
			<FormField
				v-model="definitionForm.description"
				:label="$t('project.customFields.description')"
			/>
			<FormField
				v-if="!editingDefinition"
				v-model="definitionForm.machineKey"
				:label="$t('project.customFields.machineKey')"
			/>
			<FormSelect
				v-if="!editingDefinition"
				v-model="definitionForm.fieldType"
				:label="$t('project.customFields.fieldType')"
				:options="fieldTypeOptions"
			/>
			<template v-if="definitionForm.fieldType === 'number'">
				<FormField
					v-model="definitionForm.precision"
					:label="$t('project.customFields.precision')"
				/>
				<FormField
					v-model="definitionForm.min"
					:label="$t('project.customFields.min')"
				/>
				<FormField
					v-model="definitionForm.max"
					:label="$t('project.customFields.max')"
				/>
				<FormField
					v-model="definitionForm.step"
					:label="$t('project.customFields.step')"
				/>
				<FormField
					v-model="definitionForm.unit"
					:label="$t('project.customFields.unit')"
				/>
			</template>
			<FancyCheckbox
				v-model="definitionForm.showOnCard"
				class="mbe-2"
			>
				{{ $t('project.customFields.showOnCard') }}
			</FancyCheckbox>
			<FancyCheckbox
				v-model="definitionForm.showInTable"
				class="mbe-2"
			>
				{{ $t('project.customFields.showInTable') }}
			</FancyCheckbox>
			<div class="is-flex is-justify-content-end">
				<XButton
					variant="tertiary"
					class="mie-2"
					@click="showCreateForm = false"
				>
					{{ $t('misc.cancel') }}
				</XButton>
				<XButton
					variant="primary"
					@click="saveDefinition"
				>
					{{ $t('misc.save') }}
				</XButton>
			</div>
		</div>

		<div
			v-if="definitions.length > 0"
			class="has-horizontal-overflow"
		>
			<table class="table has-actions is-striped is-hoverable is-fullwidth">
				<thead>
					<tr>
						<th>{{ $t('project.customFields.title') }}</th>
						<th>{{ $t('project.customFields.fieldType') }}</th>
						<th>{{ $t('project.customFields.machineKey') }}</th>
						<th>{{ $t('project.customFields.status') }}</th>
						<th class="has-text-end">
							{{ $t('project.customFields.actions') }}
						</th>
					</tr>
				</thead>
				<tbody>
					<tr
						v-for="def in definitions"
						:key="def.id"
					>
						<td>{{ def.title }}</td>
						<td>{{ t(`project.customFields.types.${def.fieldType}`) }}</td>
						<td><code>{{ def.machineKey }}</code></td>
						<td>
							<span
								v-if="def.isArchived"
								class="tag is-warning"
							>
								{{ $t('project.customFields.archived') }}
							</span>
							<span
								v-else
								class="tag is-success"
							>
								{{ $t('project.customFields.active') }}
							</span>
						</td>
						<td class="has-text-end actions">
							<XButton
								v-if="isSelectFieldType(def.fieldType) && !def.isArchived"
								class="mie-2"
								:aria-label="$t('project.customFields.options')"
								icon="list"
								@click="openOptions(def)"
							/>
							<XButton
								class="mie-2"
								:aria-label="$t('project.customFields.edit')"
								icon="pen"
								@click="startEdit(def)"
							/>
							<XButton
								class="mie-2"
								:aria-label="def.isArchived ? $t('project.customFields.unarchive') : $t('project.customFields.archive')"
								:icon="def.isArchived ? 'undo' : 'archive'"
								@click="toggleArchive(def)"
							/>
							<XButton
								class="is-danger"
								:aria-label="$t('project.customFields.delete')"
								icon="trash-alt"
								@click="startDelete(def)"
							/>
						</td>
					</tr>
				</tbody>
			</table>
		</div>
	</CreateEdit>

	<Modal
		:enabled="showDeleteModal"
		@close="showDeleteModal = false"
		@submit="confirmDelete"
	>
		<template #header>
			<span>{{ $t('project.customFields.delete') }}</span>
		</template>
		<template #text>
			<p>{{ $t('project.customFields.deleteText') }}</p>
			<FancyCheckbox
				v-if="deleteConflict"
				v-model="deleteValues"
				class="mbs-2"
			>
				{{ $t('project.customFields.deleteValues') }}
			</FancyCheckbox>
		</template>
	</Modal>

	<Modal
		:enabled="showOptionsModal"
		@close="showOptionsModal = false"
	>
		<template #header>
			<span>{{ $t('project.customFields.options') }}</span>
		</template>
		<template #text>
			<div class="box">
				<FormField
					v-model="optionForm.label"
					:label="$t('project.customFields.optionLabel')"
				/>
				<FormField
					v-if="!editingOption"
					v-model="optionForm.machineKey"
					:label="$t('project.customFields.machineKey')"
				/>
				<FormField
					v-model="optionForm.hexColor"
					:label="$t('project.customFields.optionColor')"
				/>
				<XButton
					variant="primary"
					@click="saveOption"
				>
					{{ $t('misc.save') }}
				</XButton>
			</div>
			<table class="table is-striped is-hoverable is-fullwidth">
				<thead>
					<tr>
						<th>{{ $t('project.customFields.optionLabel') }}</th>
						<th>{{ $t('project.customFields.status') }}</th>
						<th class="has-text-end">
							{{ $t('project.customFields.actions') }}
						</th>
					</tr>
				</thead>
				<tbody>
					<tr
						v-for="opt in options"
						:key="opt.id"
					>
						<td>
							<span
								v-if="opt.hexColor"
								class="color-dot"
								:style="{backgroundColor: '#' + opt.hexColor}"
							/>
							{{ opt.label }}
						</td>
						<td>
							<span
								v-if="opt.isArchived"
								class="tag is-warning"
							>
								{{ $t('project.customFields.archived') }}
							</span>
							<span
								v-else
								class="tag is-success"
							>
								{{ $t('project.customFields.active') }}
							</span>
						</td>
						<td class="has-text-end">
							<XButton
								class="mie-2"
								:aria-label="$t('project.customFields.edit')"
								icon="pen"
								@click="startEditOption(opt)"
							/>
							<XButton
								:aria-label="opt.isArchived ? $t('project.customFields.unarchive') : $t('project.customFields.archive')"
								:icon="opt.isArchived ? 'undo' : 'archive'"
								@click="toggleOptionArchive(opt)"
							/>
						</td>
					</tr>
				</tbody>
			</table>
		</template>
	</Modal>
</template>

<style lang="scss" scoped>
.actions {
	display: flex;
	align-items: center;
	justify-content: flex-end;
}

.color-dot {
	display: inline-block;
	inline-size: .75rem;
	block-size: .75rem;
	border-radius: 50%;
	margin-inline-end: .5rem;
	vertical-align: middle;
}
</style>
