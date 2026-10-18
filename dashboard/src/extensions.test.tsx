import {describe,it,expect,vi} from 'vitest';
import {render,screen,fireEvent} from '@testing-library/react';
import {pathToState,stateToPath} from './modules/shell/routing';
import {validateExtensions,type ExtensionPage} from './extensions';
import {Sidebar} from './modules/shell/Sidebar';
import {SettingsPage} from './modules/settings';
Object.defineProperty(window, 'matchMedia', { writable: true, value: vi.fn().mockImplementation(query => ({matches: false, media: query, addEventListener: vi.fn(), removeEventListener: vi.fn()})) });
const pages:ExtensionPage[]=[{id:'extension:billing',label:'Billing',component:()=> <div>Billing content</div>}];
describe('application extensions',()=>{
 it('round trips extension deep links and rejects unregistered pages',()=>{
  expect(stateToPath('extension:billing',{},pages)).toBe('/extensions/billing');
  expect(pathToState('/extensions/billing',pages)?.view).toBe('extension:billing');
  expect(pathToState('/extensions/billing')).toBeNull();
  expect(pathToState('/workflows/%invalid')).toBeNull();
  expect(pathToState('/traces/a%2Fb')?.selected.traceId).toBe('a/b');
 });
 it('refuses duplicate and invalid extension identifiers',()=>{
  expect(()=>validateExtensions({pages:[...pages,...pages]})).toThrow();
  expect(()=>validateExtensions({pages:[{...pages[0],id:'extension:../auth'}]})).toThrow();
 });
 it('adds navigation without replacing core pages',()=>{
  const views:string[]=[];
  render(<Sidebar items={pages} currentView="overview" setView={v=>views.push(v)} workspace={null} workspaces={[]} setWorkspace={()=>{}} createWorkspace={()=>{}} deleteWorkspace={async()=>{}}/>);
  expect(screen.getByRole('button',{name:'Overview'})).toBeTruthy();
  fireEvent.click(screen.getByRole('button',{name:'Billing'}));
  expect(views).toEqual(['extension:billing']);
 });
 it('renders a registered settings section',()=>{
  render(<SettingsPage sections={[{id:'extension:billing',label:'Subscription',component:()=> <p>Subscription settings</p>}]}/>);
  fireEvent.click(screen.getByRole('button',{name:'Subscription'}));
  expect(screen.getByText('Subscription settings')).toBeTruthy();
 });
});
