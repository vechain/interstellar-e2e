module.exports = {
  require: ['ts-node/register', 'src/globalSetup.ts'],
  extensions: ['ts'],
  spec: ['test/**/*.test.ts'],
  timeout: 120000,
  reporter: 'spec',
  exit: true,
};
