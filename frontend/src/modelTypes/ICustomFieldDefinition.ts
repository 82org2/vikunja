import type {IAbstract} from './IAbstract'
import type {ICustomFieldValue} from './ICustomFieldValue'

export interface ICustomFieldConfiguration {
	precision?: number
	min?: string
	max?: string
	step?: string
	unit?: string
}

export interface ICustomFieldDefinition extends IAbstract {
	id: number
	projectId: number
	machineKey: string
	title: string
	description: string
	fieldType: string
	isArchived: boolean
	position: number
	showOnCard: boolean
	showInTable: boolean
	configuration: ICustomFieldConfiguration | null
	defaultValue: ICustomFieldValue | null
	created: Date
	updated: Date
}
