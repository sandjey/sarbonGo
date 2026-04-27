--
-- PostgreSQL database dump
--

\restrict sMvXTKh2VUdcDFnAPRclmX61hJ7c8L8Kx8s2MEsLDiXFwocSaVHuw9YNntTnbOi

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
-- Name: goadmin_role_menu; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.goadmin_role_menu (
    role_id integer NOT NULL,
    menu_id integer NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP
);


--
-- Name: goadmin_role_menu goadmin_role_menu_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.goadmin_role_menu
    ADD CONSTRAINT goadmin_role_menu_unique UNIQUE (role_id, menu_id);


--
-- Name: goadmin_role_menu_role_id_menu_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX goadmin_role_menu_role_id_menu_id_idx ON public.goadmin_role_menu USING btree (role_id, menu_id);


--
-- PostgreSQL database dump complete
--

\unrestrict sMvXTKh2VUdcDFnAPRclmX61hJ7c8L8Kx8s2MEsLDiXFwocSaVHuw9YNntTnbOi

