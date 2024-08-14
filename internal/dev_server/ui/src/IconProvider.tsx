import type { ReactNode } from 'react';
import { useEffect } from 'react';
import { IconContext } from '@launchpad-ui/icons';
import sprite from '@launchpad-ui/icons/sprite.svg';

export function IconProvider({ children }: { children: ReactNode }) {
  useEffect(() => {
    fetch(sprite)
      .then(async (response) => response.text())
      .then((data) => {
        const parser = new DOMParser();
        const content = parser.parseFromString(
          data,
          'image/svg+xml',
        ).documentElement;
        content.id = 'lp-icons-sprite';
        content.style.display = 'none';
        document.body.appendChild(content);
      })
      .catch((_err) => {
        console.log('unable to fetch icon', _err);
      });
  }, []);

  return (
    <IconContext.Provider value={{ path: '' }}>{children}</IconContext.Provider>
  );
}
