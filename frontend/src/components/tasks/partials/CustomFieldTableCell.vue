<template>
	<td>
		<template v-if="value">
			<span
				v-if="value.type === 'short_text'"
				class="custom-field-value"
			>{{ value.shortText }}</span>
			<span
				v-else-if="value.type === 'long_text'"
				class="custom-field-value"
			>{{ value.longText }}</span>
			<span
				v-else-if="value.type === 'number'"
				class="custom-field-value"
			>{{ formatNumber(value.number) }}</span>
			<span
				v-else-if="value.type === 'boolean'"
				class="custom-field-value"
			>
				<Icon
					:icon="value.boolean ? 'check' : 'times'"
					:class="value.boolean ? 'has-text-success' : 'has-text-grey'"
				/>
			</span>
			<span
				v-else-if="value.type === 'date'"
				class="custom-field-value"
			>{{ value.date }}</span>
			<span
				v-else-if="value.type === 'datetime'"
				class="custom-field-value"
			>{{ formatDateTime(value.datetime) }}</span>
			<a
				v-else-if="value.type === 'url'"
				:href="value.url"
				target="_blank"
				rel="noopener noreferrer"
				class="custom-field-value"
			>{{ value.url }}</a>
			<span
				v-else-if="value.type === 'single_select'"
				class="custom-field-value"
			>
				<span
					v-if="option"
					class="option-chip"
					:style="option.hexColor ? {backgroundColor: '#' + option.hexColor} : {}"
				>{{ option.label }}</span>
			</span>
			<span
				v-else-if="value.type === 'multi_select'"
				class="custom-field-value multi-select"
			>
				<span
					v-for="opt in selectedOptions"
					:key="opt.id"
					class="option-chip"
					:style="opt.hexColor ? {backgroundColor: '#' + opt.hexColor} : {}"
				>{{ opt.label }}</span>
			</span>
			<span
				v-else-if="value.type === 'user'"
				class="custom-field-value"
			>#{{ value.userId }}</span>
		</template>
		<span
			v-else
			class="has-text-grey-light"
		>-</span>
	</td>
</template>

<script setup lang="ts">
import {computed} from 'vue'
import Icon from '@/components/misc/Icon'
import type {ICustomFieldValue} from '@/modelTypes/ICustomFieldValue'
import type {ICustomFieldOption} from '@/modelTypes/ICustomFieldOption'
import {useCustomFieldRegistryStore} from '@/stores/customFieldRegistry'

const props = defineProps<{
	value: ICustomFieldValue | null | undefined,
	definitionId?: number,
}>()

const registry = useCustomFieldRegistryStore()

const option = computed<ICustomFieldOption | undefined>(() => {
	if (props.value?.type !== 'single_select' || typeof props.value.singleOptionId === 'undefined') {
		return undefined
	}
	return registry.getOptionById(props.value.singleOptionId)
})

const selectedOptions = computed<ICustomFieldOption[]>(() => {
	if (props.value?.type !== 'multi_select' || !props.value.optionIds) {
		return []
	}
	return props.value.optionIds
		.map(id => registry.getOptionById(id))
		.filter((o): o is ICustomFieldOption => typeof o !== 'undefined')
})

function formatNumber(n: number | undefined): string {
	return typeof n === 'undefined' ? '' : String(n)
}

function formatDateTime(dt: string | undefined): string {
	if (!dt) {
		return ''
	}
	return new Date(dt).toLocaleString()
}
</script>

<style lang="scss" scoped>
.custom-field-value {
	display: inline-flex;
	align-items: center;
	gap: .25rem;
	word-break: break-word;
}

.option-chip {
	display: inline-block;
	padding: .1rem .5rem;
	border-radius: $radius;
	font-size: .8rem;
	background: var(--grey-100);
}

.multi-select {
	flex-wrap: wrap;
}
</style>
