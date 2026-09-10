import {Factory} from '../support/factory'

export class CustomFieldValueFactory extends Factory {
	static table = 'custom_field_values'

	static factory() {
		const now = new Date()
		return {
			id: '{increment}',
			task_id: 1,
			definition_id: 1,
			value_short_text: 'value',
			created: now.toISOString(),
			updated: now.toISOString(),
		}
	}
}
