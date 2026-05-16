import React from 'react';
import { MemoryRouter } from 'react-router-dom';
import { AppRootProps, PluginType } from '@grafana/data';
import { render, waitFor } from '@testing-library/react';
import App from './App';

describe('Components/App', () => {
  let props: AppRootProps;

  beforeEach(() => {
    jest.resetAllMocks();

    props = {
      basename: 'a/sample-app',
      meta: {
        id: 'sample-app',
        name: 'Sample App',
        type: PluginType.app,
        enabled: true,
        jsonData: {},
      },
      query: {},
      path: '',
      onNavChanged: jest.fn(),
    } as unknown as AppRootProps;
  });

  test('renders without an error', async () => {
    const { queryByRole } = render(
      <MemoryRouter>
        <App {...props} />
      </MemoryRouter>
    );

    // The App page renders "Ingero" in the heading, body copy, and
    // link text. Pin the assertion to the h2 so it is not sensitive
    // to copy edits elsewhere on the page.
    await waitFor(
      () => expect(queryByRole('heading', { level: 2, name: 'Ingero' })).toBeInTheDocument(),
      { timeout: 2000 }
    );
  });
});
