import {test, expect} from '../../support/fixtures'
import {ProjectFactory} from '../../factories/project'
import {CustomFieldDefinitionFactory} from '../../factories/custom_field_definition'

test.describe('Project custom fields settings', () => {
	test.beforeEach(async ({currentUser}) => {
		await ProjectFactory.create(1, {id: 1, owner_id: currentUser.id}, false)
	})

	test('lists seeded definitions', async ({authenticatedPage: page}) => {
		await CustomFieldDefinitionFactory.create(1, {
			project_id: 1,
			machine_key: 'priority',
			title: 'Priority',
			field_type: 'single_select',
		})

		await page.goto('/projects/1/settings/custom-fields')
		await page.waitForLoadState('networkidle')

		await expect(page.locator('table.table tbody tr', {hasText: 'Priority'})).toBeVisible()
		await expect(page.locator('table.table tbody tr', {hasText: 'priority'})).toBeVisible()
	})

	test('creates a custom field definition', async ({authenticatedPage: page}) => {
		await page.goto('/projects/1/settings/custom-fields')
		await page.waitForLoadState('networkidle')

		await page.getByRole('button', {name: /create custom field/i}).click()
		await page.getByRole('textbox', {name: 'Title'}).fill('Release train')
		await page.getByRole('textbox', {name: 'Machine key'}).fill('release_train')

		const created = page.waitForResponse(r =>
			r.url().includes('/projects/1/custom-field-definitions') && r.request().method() === 'POST',
		)
		await page.getByRole('button', {name: /save/i}).click()
		await created

		await expect(page.locator('table.table tbody tr', {hasText: 'Release train'})).toBeVisible()
	})

	test('archives and unarchives a definition', async ({authenticatedPage: page}) => {
		await CustomFieldDefinitionFactory.create(1, {
			project_id: 1,
			machine_key: 'priority',
			title: 'Priority',
			field_type: 'short_text',
		})

		await page.goto('/projects/1/settings/custom-fields')
		await page.waitForLoadState('networkidle')

		const row = page.locator('table.table tbody tr', {hasText: 'Priority'})
		await row.locator('button[aria-label="Archive"]').click()
		await expect(row).toContainText('Archived')

		await row.locator('button[aria-label="Unarchive"]').click()
		await expect(row).toContainText('Active')
	})
})
