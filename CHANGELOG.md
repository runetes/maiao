# Changelog

## [1.4.0](https://github.com/runetes/maiao/compare/maiao-v1.3.0...maiao-v1.4.0) (2026-08-25)


### Features

* add Bitbucket Cloud pull request support ([#17](https://github.com/runetes/maiao/issues/17)) ([c71fa4b](https://github.com/runetes/maiao/commit/c71fa4b3d6b4a6ac97af700e2997b97270d0b782))
* add Cursor Origin provider support (beta) ([298a2cb](https://github.com/runetes/maiao/commit/298a2cb9e44ea6754d1a74db52a7ce2b9deb48a3))
* add Forgejo pull request support ([#16](https://github.com/runetes/maiao/issues/16)) ([99a3a91](https://github.com/runetes/maiao/commit/99a3a91d0fccb4f62d0114a9ed8b2df736aa3c82))
* add Gitea pull request support ([#15](https://github.com/runetes/maiao/issues/15)) ([0b60da7](https://github.com/runetes/maiao/commit/0b60da76be42e8f858b87147de79909099f7f423))
* add GitLab merge request support ([16cd79f](https://github.com/runetes/maiao/commit/16cd79fed708e3934f75ee88f1969261044625ba))
* add provider detection infrastructure for multi-provider support ([89fe14b](https://github.com/runetes/maiao/commit/89fe14b9c309a72ef3d8133b1195fc0ef5f3a1fb))
* add provider-aware credential resolution ([97cd9cf](https://github.com/runetes/maiao/commit/97cd9cf26efce13b9c4053ce4e84e7081bfdaedd))


### Bug Fixes

* auto-resolve missing remote HEAD ref for default branch detection ([8d9ade9](https://github.com/runetes/maiao/commit/8d9ade9c07b34de13078b119eda0d53ee2673243))
* auto-resolve SSH known_hosts errors with interactive prompt ([7b11749](https://github.com/runetes/maiao/commit/7b117490a10439af0c1c61b1d50492ff63e47825))
* handle GitHub native stacks base branch conflict ([#24](https://github.com/runetes/maiao/issues/24)) ([53e6c71](https://github.com/runetes/maiao/commit/53e6c71e82a0338da1d414871aa0f3dc2e0d8270))
* surface actual error when provider client creation fails ([#21](https://github.com/runetes/maiao/issues/21)) ([05c7dcc](https://github.com/runetes/maiao/commit/05c7dcca3a07e997ae0b7f779c6c786d01aa0d3d))

## [1.3.0](https://github.com/runetes/maiao/compare/maiao-v1.2.2...maiao-v1.3.0) (2026-08-22)


### Features

* add Stack option to ReviewOptions with git config support ([5fbbba9](https://github.com/runetes/maiao/commit/5fbbba9377ea344798cbf0abf3b33fd4799e9b02))
* automate releases with release-please and formula updates ([99fd4ea](https://github.com/runetes/maiao/commit/99fd4eafe832714fbdca8defb016f57ebfba79cc))
* automate releases with release-please and formula updates ([45bbccf](https://github.com/runetes/maiao/commit/45bbccf39af8c70ec06b3220fe70e95579df8e2c))
* implement StackManager interface with disk-cached availability ([247e72b](https://github.com/runetes/maiao/commit/247e72b70909c1bf6d5736627f7a3d209541c590))
* implement StackManager interface with disk-cached availability ([5988937](https://github.com/runetes/maiao/commit/5988937cd73434bd521f7dd13c632b929a919bb8))


### Bug Fixes

* build badge ([#110](https://github.com/runetes/maiao/issues/110)) ([1243a71](https://github.com/runetes/maiao/commit/1243a7104dee73118ec19b247c3ff4d2124c6461))
* invalid auth when using ssh ([#73](https://github.com/runetes/maiao/issues/73)) ([1c57e5e](https://github.com/runetes/maiao/commit/1c57e5e5de2d614126f5c4bac53c1b8ac8719b06))
* md docs ([#1](https://github.com/runetes/maiao/issues/1)) ([9e7f216](https://github.com/runetes/maiao/commit/9e7f216e631f5d1ca171d472792510cc18bd34fe))
* remove some debug lines ([#20](https://github.com/runetes/maiao/issues/20)) ([e05902d](https://github.com/runetes/maiao/commit/e05902d348884dcbbc924ec93255129e9cff47f3))
