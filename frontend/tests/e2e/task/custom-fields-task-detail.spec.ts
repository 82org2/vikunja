import {test, expect} from '../../support/fixtures'
import {ProjectFactory} from '../../factories/project'
import {TaskFactory} from '../../factories/task'
import {CustomFieldDefinitionFactory} from '../../factories/custom_field_definition'

test.describe('Custom fields on task detail', () => {
	test.beforeEach(async ({currentUser}) => {
		await ProjectFactory.create(1, {id: 1, owner_id: currentUser.id}, false)
		await TaskFactory.create(1, {id: 1, project_id: 1, created_by_id: currentUser.id})
	})

	test('sets a short text value on a task', async ({authenticatedPage: page}) => {
		await CustomFieldDefinitionFactory.create(1, {
			project_id: 1,
			machine_key: 'release_train',
			title: 'Release train',
			field_type: 'short_text',
		})

		await page.goto('/tasks/1')
		await page.waitForLoadState('networkidle')

		await page.getByRole('button', {name: /custom fields/i}).click()
		const input = page.getByRole('textbox', {name: 'Release train'})
		await input.fill('2026-09')

		const set = page.waitForResponse(r =>
			r.url().includes('/tasks/1/custom-field-values/') && r.request().method() === 'PUT',
		)
		await input.blur()
		await set

		await expect(input).toHaveValue('2026-09')
		await expect(page.locator('.custom-field-editor', {hasText: 'Release train'})).toContainText('Unset')
	})
})
