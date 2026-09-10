import {faker} from '@faker-js/faker'
import {Factory} from '../support/factory'

export class CustomFieldOptionFactory extends Factory {
	static table = 'custom_field_options'

	static factory() {
		const now = new Date()
		return {
			id: '{increment}',
			definition_id: 1,
			machine_key: faker.lorem.slug(2).replace(/-/g, '_'),
			label: faker.lorem.words(2),
			hex_color: faker.internet.color().replace('#', ''),
			is_archived: false,
			position: 0,
			created: now.toISOString(),
			updated: now.toISOString(),
		}
	}
}
