import {Factory} from '../support/factory'

export class CustomFieldValueOptionFactory extends Factory {
	static table = 'custom_field_value_options'

	static factory() {
		const now = new Date()
		return {
			id: '{increment}',
			value_id: 1,
			option_id: 1,
			created: now.toISOString(),
		}
	}
}
