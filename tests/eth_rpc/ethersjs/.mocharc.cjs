module.exports = {
  require: ['ts-node/register'],
  extensions: ['ts'],
  spec: ['test/**/*.test.ts'],
  timeout: 120000,
  reporter: 'spec',
  exit: true,
};
