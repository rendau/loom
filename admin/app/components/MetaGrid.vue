<template>
  <!-- контекстный грид «ключ-значение» карточек (ран, даг): 2 колонки на
       узком экране, 4 на широком; элементы — MetaItem -->
  <div class="grid grid-cols-2 gap-x-8 gap-y-1.5 text-sm lg:grid-cols-4">
    <slot />
  </div>
</template>
