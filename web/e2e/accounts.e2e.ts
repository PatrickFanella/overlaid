import { expect, test } from '@playwright/test';

const tenant = { id: 'tenant-fixture', name: 'Fixture workspace', slug: 'fixture', role: 'owner', plan: 'free', retentionDays: 30 };

test('hosted login gate keeps private dashboard controls closed', async ({ page }) => {
 await page.route('**/api/account', route => route.fulfill({json:{authenticated:false,deploymentMode:'hosted',tenants:[]}}));
 await page.goto('/dashboard');
 await expect(page.getByRole('link',{name:'Sign in with Twitch',exact:true})).toBeVisible();
 await expect(page.getByRole('button',{name:'Send alert',exact:true})).toHaveCount(0);
 await expect(page.getByRole('link',{name:'Privacy policy',exact:true})).toBeVisible();
});

test('credential rotation requires consent and sends tenant and CSRF context', async ({ page }) => {
 await page.route('**/api/account', route => route.fulfill({json:{authenticated:true,deploymentMode:'hosted',accountId:'account-fixture',activeTenantId:tenant.id,csrfToken:'fixture-csrf',tenants:[tenant]}}));
 await page.route('**/api/tenants/tenant-fixture/plan', route => route.fulfill({json:{plan:'free',limits:{commands:25,timers:5}}}));
 await page.route('**/api/tenants/tenant-fixture/members', route => route.fulfill({json:{members:[{accountId:'account-fixture',displayName:'Owner',role:'owner'}]}}));
 let rotations=0;
 await page.route('**/api/tenants/tenant-fixture/credentials/overlay', route => {
  rotations++;expect(route.request().headers()['x-overlaid-csrf']).toBe('fixture-csrf');expect(route.request().headers()['x-overlaid-tenant']).toBe(tenant.id);
  expect(route.request().headers()['x-overlay-operator-secret']).toBeUndefined();
  expect(route.request().postDataJSON()).toEqual({confirm:'rotate'});
  return route.fulfill({json:{purpose:'overlay',token:'fixture-new-key'}});
 });
 await page.goto('/dashboard/account');
 await expect(page).toHaveTitle('Account and workspace · over-laid');
 const rotate=page.getByRole('button',{name:'Rotate overlay credential',exact:true});
 await expect(rotate).toBeDisabled();expect(rotations).toBe(0);
 await page.getByRole('checkbox',{name:'Confirm credential rotation; old source URLs will stop working'}).check();
 await rotate.click();
 await expect(page.getByLabel('New overlay credential')).toHaveValue('fixture-new-key');
 await expect(rotate).toBeDisabled();expect(rotations).toBe(1);
 await expect(page.getByRole('button',{name:'Delete viewer records',exact:true})).toBeDisabled();
 await expect(page.getByRole('button',{name:'Delete this workspace',exact:true})).toBeDisabled();
});

test('moderators see plan information without membership or credential controls', async ({ page }) => {
 await page.route('**/api/account', route => route.fulfill({json:{authenticated:true,deploymentMode:'hosted',accountId:'moderator-fixture',activeTenantId:tenant.id,tenants:[{...tenant,role:'moderator'}]}}));
 await page.route('**/api/tenants/tenant-fixture/plan', route => route.fulfill({json:{plan:'free',limits:{commands:25}}}));
 await page.goto('/dashboard/account');
 await expect(page.getByText('Plan and retention',{exact:true})).toBeVisible();
 await expect(page.getByRole('button',{name:'Rotate overlay credential',exact:true})).toHaveCount(0);
 await expect(page.getByRole('button',{name:'Invite or update member',exact:true})).toHaveCount(0);
 await expect(page.getByRole('button',{name:'Export workspace',exact:true})).toHaveCount(0);
});
