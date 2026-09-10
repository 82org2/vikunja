import {faker} from '@faker-js/faker'
import {Factory} from '../support/factory'

export class CustomFieldDefinitionFactory extends Factory {
	static table = 'custom_field_definitions'

	static factory() {
		const now = new Date()
		return {
			id: '{increment}',
			project_id: 1,
			machine_key: faker.lorem.slug(2).replace(/-/g, '_'),
			title: faker.lorem.words(2),
			description: '',
			field_type: 'short_text',
			is_archived: false,
			position: 0,
			show_on_card: false,
			show_in_table: false,
			created: now.toISOString(),
			updated: now.toISOString(),
		}
	}
}
