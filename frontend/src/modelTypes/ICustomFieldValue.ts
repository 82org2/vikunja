export const CUSTOM_FIELD_TYPES = {
	SHORT_TEXT: 'short_text',
	LONG_TEXT: 'long_text',
	NUMBER: 'number',
	BOOLEAN: 'boolean',
	DATE: 'date',
	DATETIME: 'datetime',
	URL: 'url',
	SINGLE_SELECT: 'single_select',
	MULTI_SELECT: 'multi_select',
	USER: 'user',
} as const
export type CustomFieldType = typeof CUSTOM_FIELD_TYPES[keyof typeof CUSTOM_FIELD_TYPES]

export const CUSTOM_FIELD_SORTABLE_TYPES = new Set<CustomFieldType>([
	CUSTOM_FIELD_TYPES.SHORT_TEXT,
	CUSTOM_FIELD_TYPES.NUMBER,
	CUSTOM_FIELD_TYPES.BOOLEAN,
	CUSTOM_FIELD_TYPES.DATE,
	CUSTOM_FIELD_TYPES.DATETIME,
	CUSTOM_FIELD_TYPES.USER,
	CUSTOM_FIELD_TYPES.SINGLE_SELECT,
])

export function isCustomFieldSortable(type: CustomFieldType | string | undefined): boolean {
	return typeof type === 'string' && CUSTOM_FIELD_SORTABLE_TYPES.has(type as CustomFieldType)
}

export interface ICustomFieldValue {
	type: string
	shortText?: string
	longText?: string
	number?: number
	boolean?: boolean
	date?: string
	datetime?: string
	url?: string
	userId?: number
	singleOptionId?: number
	optionIds?: number[]
}

export interface ITaskCustomFieldValue {
	definitionId: number
	machineKey: string
	value: ICustomFieldValue
}
