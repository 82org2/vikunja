import type {IAbstract} from './IAbstract'

export interface ICustomFieldOption extends IAbstract {
	id: number
	definitionId: number
	machineKey: string
	label: string
	hexColor: string
	isArchived: boolean
	position: number
	created: Date
	updated: Date
}
