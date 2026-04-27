--
-- PostgreSQL database dump
--

\restrict C8x9iHCbihtedMQDJa9zlNM4prrp82Z0wrTedjeByeKWodM2ziaR8ZfPGymMQiN

-- Dumped from database version 18.3
-- Dumped by pg_dump version 18.3

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: goadmin_role_users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.goadmin_role_users (
    role_id integer NOT NULL,
    user_id integer NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: goadmin_role_users goadmin_role_users_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_role_users
    ADD CONSTRAINT goadmin_role_users_unique UNIQUE (role_id, user_id);


--
-- PostgreSQL database dump complete
--

\unrestrict C8x9iHCbihtedMQDJa9zlNM4prrp82Z0wrTedjeByeKWodM2ziaR8ZfPGymMQiN

