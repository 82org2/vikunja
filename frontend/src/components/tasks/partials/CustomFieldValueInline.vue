<template>
	<span class="custom-field-value-inline">
		<template v-if="value">
			<span v-if="value.type === 'short_text'">{{ value.shortText }}</span>
			<span v-else-if="value.type === 'long_text'">{{ value.longText }}</span>
			<span v-else-if="value.type === 'number'">{{ value.number }}</span>
			<span
				v-else-if="value.type === 'boolean'"
				:class="value.boolean ? 'has-text-success' : 'has-text-grey'"
			>
				<Icon :icon="value.boolean ? 'check' : 'times'" />
			</span>
			<span v-else-if="value.type === 'date'">{{ value.date }}</span>
			<span v-else-if="value.type === 'datetime'">{{ formatDateTime(value.datetime) }}</span>
			<a
				v-else-if="value.type === 'url'"
				:href="value.url"
				target="_blank"
				rel="noopener noreferrer"
			>{{ value.url }}</a>
			<span
				v-else-if="value.type === 'single_select'"
				class="option-chip"
				:style="option ? {backgroundColor: '#' + option.hexColor} : {}"
			>{{ option?.label ?? '#' + value.singleOptionId }}</span>
			<span
				v-else-if="value.type === 'multi_select'"
				class="multi-select"
			>
				<span
					v-for="opt in selectedOptions"
					:key="opt.id"
					class="option-chip"
					:style="opt.hexColor ? {backgroundColor: '#' + opt.hexColor} : {}"
				>{{ opt.label }}</span>
			</span>
			<span v-else-if="value.type === 'user'">#{{ value.userId }}</span>
		</template>
	</span>
</template>

<script setup lang="ts">
import {computed} from 'vue'
import Icon from '@/components/misc/Icon'
import type {ICustomFieldValue} from '@/modelTypes/ICustomFieldValue'
import type {ICustomFieldOption} from '@/modelTypes/ICustomFieldOption'
import {useCustomFieldRegistryStore} from '@/stores/customFieldRegistry'

const props = defineProps<{
	value: ICustomFieldValue | null | undefined,
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

function formatDateTime(dt: string | undefined): string {
	if (!dt) {
		return ''
	}
	return new Date(dt).toLocaleString()
}
</script>

<style lang="scss" scoped>
.custom-field-value-inline {
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
	display: inline-flex;
	flex-wrap: wrap;
	gap: .25rem;
}
</style>
